# S5A3 Game Server

S5A3 游戏服务端：Go 游戏服、数据库结构与迁移、服务端部署脚本、Docs 和资源配方。

## 归属

- 上游：`Cec1c/ServerS5A3`（PVF 版本 `pvf-current-server-20261115-66482976`）
- 原资料：`01_游戏服务端-部署源码-20261119`

## 迁入范围

- go-server 源码
- DB 结构 / 迁移脚本（只保留结构与迁移，禁止提交真实数据或连接串）
- 服务端 deploy、scripts、Docs、资源配方

## 配套

- 客户端：`yuanqilei/s5a3-client`
- 登录器 / GM：`yuanqilei/purplemagic-nexus`
- 部署工具：`yuanqilei/s5a3-deploy-tool`

服务端与客户端分别发布，以同一份兼容清单锁定版本组合。

## 禁止提交

口令、Token、私钥、证书私钥、真实 `.env`、数据库连接串、玩家数据、数据库备份、data2、runtime、线上配置、本机 DPAPI 凭据。配置示例一律用占位值。

## 许可证

保留上游 `Cec1c/ServerS5A3` 的许可证、版权和来源记录。
