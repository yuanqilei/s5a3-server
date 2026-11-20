package dnfbridge

// GM 面板的「账号/角色编辑态」在上游是靠数据库行锁（AccountEditFence）实现的，
// 而那个 fence 只在 DNFBRIDGE_PLAYER_STRICT / DNFBRIDGE_GM_EDIT_PROTECTION
// 打开时存在。本产品是客户端直连 7001 + 账号密码登录，打开保护会要求每条连接
// 都带登录令牌，直连玩家将无法进入游戏 —— 所以开关不能开，fence 永远不存在，
// 上游那两个动作会一直回「不可用」，面板写操作全部 503。
//
// 这里用会话表兜底：fence 可用时上游根本不会调用到本文件（钩子在 early-return
// 里），严格模式语义不受影响。

import (
	"errors"
	"strings"
	"longheng.io/server/internal/modules/dnf/adminapi"
)

// s5GMAccountEditFallback：fence 不可用时的替代实现，语义与上游一致——
// 「这个账号现在有没有人在线」，只是数据源换成会话表而不是数据库行锁。
func (s *Service) s5GMAccountEditFallback(account string) (*adminapi.AccountEditStatus, error) {
	if account == "" || len(account) > 128 || strings.TrimSpace(account) != account {
		return nil, errors.New("invalid account identity")
	}
	out := &adminapi.AccountEditStatus{Protocol: adminapi.AccountEditProtocol, AccountID: account}
	if s == nil {
		return out, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, session := range s.liveGameSessions {
		if session != nil && session.accountID == account {
			out.Busy = true
			return out, nil
		}
	}
	for _, session := range s.gameSessions {
		if session != nil && session.accountID == account {
			out.Busy = true
			break
		}
	}
	return out, nil
}

// s5GMCharacterEditFallback：同上，按角色号判忙（含第二条传输）。
func (s *Service) s5GMCharacterEditFallback(account string, character uint16) (*adminapi.CharacterEditStatus, error) {
	if character == 0 || account == "" || len(account) > 128 || strings.TrimSpace(account) != account {
		return nil, errors.New("invalid character identity")
	}
	out := &adminapi.CharacterEditStatus{
		Protocol:    adminapi.CharacterEditProtocol,
		AccountID:   account,
		CharacterID: character,
	}
	if s == nil {
		return out, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gameSessions[character] != nil {
		out.Busy = true
	}
	for _, session := range s.liveGameSessions {
		if session != nil && session.selectedCharacterID == character {
			out.Busy = true
			break
		}
	}
	return out, nil
}
