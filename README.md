# Monitor Dashboard (网页监控与预警系统)

[![Go Version](https://img.shields.io/badge/Go-1.21%2B-blue.svg)](https://golang.org)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)

一个基于 Go 语言开发的现代化网页内容监控服务。它能够自动抓取指定网页（如公告、新闻、政策），并在发现匹配关键字的新内容时，通过企业微信 Webhook 发送实时通知。系统配备了功能强大的 Web 管理后台，支持多目标管理、可视化配置及历史记录查询。

## 🌟 核心功能

-   **多目标并行监控**: 支持同时设置多个监控目标，每个目标可独立配置抓取规则。
-   **智能内容分析**:
    -   **动态渲染支持**: 内置 Headless Chrome (Chromedp) 引擎，完美处理 JavaScript 异步加载的动态页面。
    -   **关键字过滤**: 支持多关键字匹配，仅推送感兴趣的内容。
    -   **过期自动过滤**: 可设置忽略 N 天前的旧记录，避免历史冗余通知。
-   **现代化 Web 后台**:
    -   **实时仪表盘**: 监控系统运行状态及最新抓取日志。
    -   **历史记录管理**: 完整的通知历史，支持按来源筛选、多种日期排序、搜索及分页。
    -   **手动重推 (Repush)**: 漏看通知？一键手动重新发送历史记录到企业微信。
-   **灵活配置**:
    -   **热重载**: 在线修改 Webhook 地址及检查间隔，系统自动调整频率，无需重启。
    -   **一键启停**: 界面化管理监控开关，启用时自动触发即时检查。
-   **数据安全**: 使用高性能 SQLite 3 数据库持久化所有配置与历史。

## 🛠️ 技术栈

| 组件 | 技术方案 |
| :--- | :--- |
| **后端** | Go 1.21+, SQLite 3 (CGO-free), Chromedp, Goquery |
| **前端** | 原生 JavaScript (ES6+), CSS3 (Modern UI), HTML5 |
| **部署** | Docker, Docker Compose, Makefile |

## 🚀 快速开始

### 方法一：直接运行 (需安装 Go 和 Chrome)

1.  **克隆项目**:
    ```bash
    git clone [repository-url]
    cd monitor
    ```

2.  **编译程序**:
    ```bash
    go build -o monitor main.go
    ```

3.  **运行服务**:
    ```bash
    # 默认监听 8080 端口
    ./monitor
    ```

### 方法二：使用 Docker (推荐)

项目提供了完整的 Docker 环境配置：

```bash
# 使用 Docker Compose 启动
docker-compose up -d
```

## ⚙️ 配置说明

### 1. 通用配置 (Generic Config)
在 Web 后台的“通用配置”标签页中设置：
-   **Webhook URL**: 企业微信机器人的 Webhook 地址。
-   **检查间隔**: 监控轮询的频率（单位：秒）。
-   **忽略天数**: 超过此天数的公告将不再发送通知。

### 2. 环境变量
可以通过环境变量覆盖默认配置：
-   `WEBHOOK_URL`: 企业微信 Webhook
-   `DB_FILE`: 数据库文件路径 (默认为 `monitor.db`)

## 📂 项目结构

```text
├── main.go            # 后端核心逻辑 (API, 爬虫引擎, 数据库管理)
├── config.json        # 种子配置文件 (仅首次运行用于初始化 DB)
├── monitor.db         # SQLite 数据文件 (自动生成)
├── static/            # Web 管理后台静态资源
│   ├── index.html     # 后台主页面
│   ├── app.js         # 前端交互逻辑
│   └── style.css      # 现代样式定义
├── Dockerfile         # 镜像构建文件
└── docker-compose.yml # 容器编排定义
```

## 📝 注意事项

-   **Chrome 依赖**: 本程序依赖本地或容器内的 Chrome/Chromium 浏览器。
-   **性能提示**: 过于频繁的检查间隔（如 < 60秒）可能导致反爬机制拦截，建议保持默认或按需调整。
-   **初始化**: 首次运行时，若 `config.json` 存在，系统会将其中的 targets 导入 SQLite。后续所有修改均以数据库为准。

## 📜 许可证

本项目采用 MIT 许可证，详情见 `LICENSE` 文件。
