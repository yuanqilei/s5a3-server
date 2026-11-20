---
category: 功能修复
type: change
---

# docker profile：支持公网/NAT 地址（opt-in）

新增两个显式开关，默认行为完全不变（仍是"仅私网 LAN"）：

- `--advertise-ip`：下发给客户端的地址（`public_addr`、`DNFBRIDGE_SERVER_IP`），
  允许公网/EIP 地址；未指定时仍用自动探测的私网 `host_ip`。
- `--bind-ip`：游戏端口发布与 7001 监听的绑定地址，默认仍为 `host_ip`；
  显式传 `0.0.0.0` 才发布到通配地址。

两者也可直接写在 `runtime/docker/instance.json` 的 `advertise_ip` / `bind_ip`，
之后的 `prepare` 会沿用，便于在 NAT 云主机（阿里云 EIP 等）上重复部署。

`17002`（账号 API）与 `18111`（管理 API）仍固定只监听 `127.0.0.1`。

> 说明：这与 `AGENTS.md` 中"local profile 只用私网地址、禁止通配/公网"的约定相反，
> 属于本服公网部署的显式偏离；不传参数时行为与该约定一致。
