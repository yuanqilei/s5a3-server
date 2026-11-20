---
category: 功能修复
type: change
---

# docker profile：生成并下发玩家代理密钥

prepare 现在会生成 player_proxy_secret（hex 48）并写入 server.env：

- DNFBRIDGE_PLAYER_STRICT=true
- DNFBRIDGE_PLAYER_PROXY_SECRET=<secret>

网关（PurpleMagic）的 game.proxySecret 必须与该值一致；game.adminToken
对应已有的 DNFBRIDGE_PLAYER_ADMIN_SECRET。缺这项时严格模式会让账号转发被拒。
