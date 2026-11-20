// 本文件由 deploy_s5a3.sh 在构建镜像前注入到 go-server/cmd/server/dnf90/，
// 不属于上游仓库。它和 main.go 同属 package main，靠 Go 的 init() 自启动，
// 因此上游一行代码都不用改（Dockerfile 是 COPY go-server/ ./ +
// go build ./cmd/server/dnf90，同目录下的 .go 全都会被编进去）。
//
// 为什么需要它：
//   Go 默认 GOGC=100，堆涨到「存活堆 × 2」才回收；回收之后空闲页也只归还一部分。
//   DNF 服务端加载 117MB PVF 后存活堆约 1.9GB，于是容器 RSS 会一路爬到 5-6GB，
//   在 GOMEMLIMIT 附近才停住 —— 多出来的那几 GB 是垃圾，不是真在用。
//
// 它做三件事：
//  1. 空闲页堆积到阈值（默认 384MB）时调 debug.FreeOSMemory() 把内存还给 OS，
//     两次之间至少隔 30 分钟，避免频繁 STW 造成卡顿。
//  2. 每 6 小时比一次堆谷值，持续抬高才告警 —— 那种是真泄漏，回收救不了。
//  3. 收到 SIGUSR1（docker exec <容器> kill -USR1 1）立即回收一次。
//
// 全部逻辑跑在独立 goroutine 里，整段包了 recover：任何异常只让看护停下，
// 不影响服务端主流程。
package main

import (
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	// 启动阶段 PVF 加载本身就要吃掉两三个 GB，这时插回收只会白挨一次停顿。
	mgStartDelay = 45 * time.Second
	mgTick       = 60 * time.Second
	mgMinTick    = 15 * time.Second
	// 空闲但没还给 OS 的内存达到这个量才值得动一次手。
	mgIdleMB       = 384
	mgMinGap       = 30 * time.Minute
	mgLeakWindow   = 6 * time.Hour
	mgLeakGrowth   = 20.0 // 堆谷值涨幅超过这个百分比才告警
	mgLeakFloor    = 64 << 20
	mgUnlimitedCap = 1 << 62
)

var mgOnce sync.Once

func init() {
	if mgDisabled() {
		return
	}
	mgOnce.Do(func() {
		go mgWatchSignal()
		go mgLoop()
	})
}

func mgDisabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("S5_MEM_GUARD"))) {
	case "0", "off", "false", "no", "disable", "disabled":
		return true
	}
	return false
}

func mgEnvInt(name string, def int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

// 统一前缀，方便 docker logs | grep memguard。走 stderr 是因为上游日志本来就收 stderr。
func mgLogf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[memguard] "+format+"\n", args...)
}

func mgMB(b int64) int64 {
	if b <= 0 {
		return 0
	}
	return b >> 20
}

// mgRSS 从 /proc/self/statm 读常驻页数。读不到（非 Linux）返回 fallback，
// 只影响日志展示，不参与回收判断。
func mgRSS(fallback int64) int64 {
	data, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return fallback
	}
	fields := strings.Fields(string(data))
	if len(fields) < 2 {
		return fallback
	}
	pages, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil || pages <= 0 {
		return fallback
	}
	return pages * int64(os.Getpagesize())
}

func mgLoop() {
	defer func() {
		if r := recover(); r != nil {
			mgLogf("看护协程异常退出（服务端不受影响）：%v", r)
		}
	}()

	tick := time.Duration(mgEnvInt("S5_MEM_GUARD_INTERVAL_SEC", int(mgTick/time.Second))) * time.Second
	if tick < mgMinTick {
		tick = mgMinTick
	}
	idleTrigger := int64(mgEnvInt("S5_MEM_GUARD_IDLE_MB", mgIdleMB)) << 20
	minGap := time.Duration(mgEnvInt("S5_MEM_GUARD_MIN_GAP_SEC", int(mgMinGap/time.Second))) * time.Second
	delay := time.Duration(mgEnvInt("S5_MEM_GUARD_START_DELAY_SEC", int(mgStartDelay/time.Second))) * time.Second

	time.Sleep(delay)
	mgLogf("已启用：每 %s 检查一次，空闲页 ≥ %d MB 才回收，两次间隔 ≥ %s；kill -USR1 1 可立即回收",
		tick, idleTrigger>>20, minGap)

	var (
		stats    runtime.MemStats
		lastFree time.Time
		lastWarn time.Time
		baseMin  int64
		curMin   int64
		baseAt   = time.Now()
		reclaims int
	)

	for {
		time.Sleep(tick)
		runtime.ReadMemStats(&stats)
		now := time.Now()
		alloc := int64(stats.HeapAlloc)

		// —— 泄漏趋势：只看每轮的低点。低点持续抬高才是底噪在涨，
		//    那些「跟着玩家数量上下浮动」的峰值不算。
		if curMin == 0 || alloc < curMin {
			curMin = alloc
		}
		if baseMin == 0 {
			baseMin = curMin
		}
		if now.Sub(baseAt) >= mgLeakWindow {
			if baseMin >= mgLeakFloor && curMin > baseMin {
				grow := float64(curMin-baseMin) / float64(baseMin) * 100
				if grow >= mgLeakGrowth && now.Sub(lastWarn) >= mgLeakWindow {
					mgLogf("警告：最近 %s 堆谷值 %d MB → %d MB（+%.0f%%），疑似真实泄漏；"+
						"这种回收救不了，开 debug.pprof_enabled 后抓 /debug/pprof/heap 才能定位",
						mgLeakWindow, mgMB(baseMin), mgMB(curMin), grow)
					lastWarn = now
				}
			}
			baseMin, curMin, baseAt = curMin, 0, now
		}

		// —— 主动回收：只处理「已经空闲、但还攥在 Go 手里」的那部分。
		//    HeapIdle 是空闲 span 总量，HeapReleased 是已经还给内核的量，差值是欠账。
		idleHeld := int64(stats.HeapIdle) - int64(stats.HeapReleased)
		if idleHeld < idleTrigger {
			continue
		}
		if !lastFree.IsZero() && now.Sub(lastFree) < minGap {
			continue
		}
		// 堆已经贴着软上限时 runtime 正在高频 GC，这时候再插一次只会火上浇油。
		if limit := debug.SetMemoryLimit(-1); limit > 0 && limit < mgUnlimitedCap && alloc > limit*9/10 {
			continue
		}

		beforeAlloc, beforeRSS := alloc, mgRSS(int64(stats.Sys))
		started := time.Now()
		debug.FreeOSMemory()
		lastFree, reclaims = now, reclaims+1

		var after runtime.MemStats
		runtime.ReadMemStats(&after)
		mgLogf("已回收 #%d：空闲 %d MB → 堆 %d→%d MB，RSS %d→%d MB，停顿 %s",
			reclaims, mgMB(idleHeld), mgMB(beforeAlloc), mgMB(int64(after.HeapAlloc)),
			mgMB(beforeRSS), mgMB(mgRSS(int64(stats.Sys))), time.Since(started).Round(time.Millisecond))
	}
}

// SIGUSR1 在 Linux 上是 10。用数字而不是 syscall.SIGUSR1，是为了同一份文件
// 在 Windows 上也能编过（本地 build.bat 会编 Windows 版）。
func mgWatchSignal() {
	defer func() {
		if r := recover(); r != nil {
			mgLogf("信号监听异常退出：%v", r)
		}
	}()
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.Signal(10))
	for range ch {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					mgLogf("手动回收异常：%v", r)
				}
			}()
			beforeRSS := mgRSS(0)
			started := time.Now()
			debug.FreeOSMemory()
			mgLogf("手动回收：RSS %d→%d MB，停顿 %s",
				mgMB(beforeRSS), mgMB(mgRSS(0)), time.Since(started).Round(time.Millisecond))
		}()
	}
}
