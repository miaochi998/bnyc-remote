# bnyc-remote

公司自托管远程桌面辅助平台：**CrossDesk**（开源远控）+ 自研**账号 / 地址簿**服务。

## 目标

- 在公司内网自建 CrossDesk 信令（WSS）与 TURN 中继（Coturn）
- 为每个员工提供独立账号与独立地址簿：设备一次加入，之后一键远控，不再每次输 ID / 密码
- 同时提供**桌面客户端**与 **Web 客户端**两种入口
- 与现有 RustDesk 并存，互不影响，作为第二条可用通道

## 目录结构

| 目录 | 内容 |
|---|---|
| `docs/` | 设计文档、部署手册（已脱敏） |
| `portal/` | 自研账号 / 地址簿服务（Go + SQLite + 管理页） |
| `web/` | Web 客户端 overlay：`addressbook.js` / `.css`、注入脚本、上游版本锁定 |
| `client/patches/` | CrossDesk 桌面端 patch 集（不提交完整上游代码） |
| `client/build/` | 拉上游 → 打 patch → 构建 → 出包 的脚本 |
| `deploy/` | docker-compose、`.env.example`、DDNS / 备份 / 健康检查脚本 |
| `.github/workflows/` | CI：构建、gitleaks 扫描、发布 |

## 安全声明（重要）

本仓库是**公开仓库**，不包含任何真实网络参数、域名、IP、端口映射、凭据、证书或数据。

- 真实环境参数只保存在本地 `real-values.local.md`（已加入 `.gitignore`）
- 所有示例一律使用占位符：`example.com`、`192.0.2.x`、`203.0.113.x`
- 提交前由本地 gitleaks 与 CI 双重扫描
- **禁止提交**：`.env`、`*.pem` / `*.key` / `*.crt`、SQLite 库、设备 ID、员工信息、云厂商 AccessKey、DNSPod Token、SSH / WireGuard 私钥

## 许可

| 部分 | 许可 | 说明 |
|---|---|---|
| `portal/`、`deploy/` | MIT（或内部使用） | 自研代码 |
| `web/` | LGPL-3.0 | 与 CrossDesk Web Client 一致 |
| `client/patches/` | GPL-3.0 | 与 CrossDesk 桌面端一致 |

CrossDesk 上游：https://github.com/kunkundi/crossdesk
