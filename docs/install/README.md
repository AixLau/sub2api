# 星链 AI Hub 客户端配置教程

## 文档说明

这是**星链 AI Hub 客户端一键配置脚本**的用户教程页面，面向普通用户，提供傻瓜式的 Codex / Claude Code 客户端配置指引。

- **目标用户**：购买了星链 AI Hub 服务、需要配置客户端的普通用户
- **覆盖平台**：macOS / Linux / Windows
- **配置方式**：一键脚本（自动备份 + 交互式选择客户端 + 浏览器登录授权 + 自动创建 API Key）

## 文件结构

```
docs/install/
├── index.html          # 教程主页（单页应用，响应式设计）
├── assets/             # 配图资源
│   ├── 01-api-keys.webp         # 备用：星链 AI Hub 登录/自动授权提示图
│   ├── 02-mac-terminal.webp     # macOS 终端自动授权流程
│   ├── 03-win-powershell.webp   # Windows PowerShell 自动授权流程
│   ├── 04-win-tray-quit.webp    # Windows 托盘退出 Codex 示意图
│   ├── 05-codex-api-key-login.webp  # Codex 首次登录页
│   ├── 06-copy-codex-api-key.webp   # API 密钥复制位置
│   ├── ccswitch-01-create-key.webp  # 创建 Codex API Key
│   ├── ccswitch-02-import-action.webp  # 一键导入到 CCS
│   ├── ccswitch-03-copy-and-import.webp # 手动配置前复制 API Key
│   ├── ccswitch-04-select-codex.webp # CC Switch 选择 Codex 并新增供应商
│   ├── ccswitch-05-provider-list.webp # 选择自定义供应商
│   └── ccswitch-06-manual-config.webp # 手动填写供应商配置
└── README.md           # 本说明文件
```

## 技术栈

- **纯静态 HTML + 内联 CSS/JS**，无外部依赖，部署到任何静态服务器即可
- **WebP 图片格式**，兼顾清晰度和加载速度
- **iOS 风格动画**：Tab 切换、OS 面板 3D 翻转过渡（700ms 平滑动画）
- **响应式设计**：支持桌面 / 平板 / 手机

## 部署

### 本地预览

```bash
cd /Users/lushiwu/dev/sub2api/docs/install
python3 -m http.server 8765
# 访问 http://localhost:8765
```

### 生产部署

直接将 `docs/install/` 目录部署到 Web 服务器的静态文件目录，确保：

1. **index.html** 可通过 `https://yourdomain.com/docs/install/` 访问
2. **assets/** 子目录可访问（图片引用路径 `/docs/install/assets/*.webp`）
3. 服务器支持 `.webp` MIME 类型（现代 Web 服务器默认支持）

示例 nginx 配置：

```nginx
location /docs/install/ {
    root /var/www/sub2api;
    index index.html;
    try_files $uri $uri/ =404;
}
```

## 维护说明

### 更新截图

所有截图均使用 AI 图片生成（`~/.codex/skills/imagegen`），配置文件在生成时记录。

重新生成某张图：

```bash
cd ~/.codex/skills/imagegen
IMAGEGEN_API_KEY="your-key" python3 scripts/imagegen_interactive.py \
  --config templates/imagegen_config.toml \
  --provider-mode images \
  --images-endpoint https://aixlau.me/v1/images/generations \
  --output-dir /Users/lushiwu/dev/sub2api/docs/install/assets \
  --title "02-mac-terminal" \
  --resolution "1774x887" \
  --prompt "你的 prompt"

# 转 WebP 压缩
cwebp -q 85 output.png -o 02-mac-terminal.webp
```

### 更新教程内容

直接编辑 `index.html`，注意：

- **品牌名称**：全局使用"星链 AI Hub"，域名 `aixlau.me`
- **动画持续时间**：CSS 变量 `--duration-fast: 300ms; --duration-normal: 600ms; --duration-slow: 800ms`
- **OS 默认选择**：JS 初始化时根据 UA 自动判断（Mac/Linux → mac，其余 → win）
- **脚本 URL**：`https://aixlau.me/install/bootstrap.sh`（确保与实际部署一致）

### 图片尺寸建议

- **01-api-keys.webp**：备用图片，当前一键脚本主流程不展示登录配图
- **02-mac-terminal.webp**：1774×887（接近 2K 横屏），展示 macOS 终端选择客户端后自动授权成功
- **03-win-powershell.webp**：约 1600×900，展示 Windows PowerShell 选择客户端后自动授权成功
- **04-win-tray-quit.webp**：约 1600×900，托盘特写，使用真实 Codex 图标（蓝紫渐变花瓣 + `>_` 符号）
- **05-codex-api-key-login.webp**：Codex 首次打开仍显示登录页的处理示意图
- **06-copy-codex-api-key.webp**：用黑色箭头标出星链 AI Hub「API 密钥」页面中复制 `codex` 密钥的位置
- **ccswitch-01 ～ 06**：CC Switch 配置流程；先尝试从密钥列表一键导入，失败时复制 API Key，并在 Codex 供应商中手动填写 `https://aixlau.me`

所有图片 `max-width: 900px` 居中显示，确保大屏不会过大。

## 相关文档

- **CC Switch 配置教程**：`docs/ccswitch/` - 图形化多账号切换工具
- **一键脚本源码**：`deploy/client/` - 面向最终用户的客户端配置脚本
- **主 README**：项目根目录 `README.md`

## 设计规范

### 配色

- **主色**：`#0033ff`（星链蓝）
- **成功色**：`#0a7c3b`
- **警告色**：`#b54708`
- **背景**：`#ffffff` / `#f8f9fb`
- **文本**：`#212121` / `#333333`

### 交互动效

- **Tab 切换**：上浮淡入（translateY + opacity，600ms）
- **OS 切换**：3D 翻转（rotateY ±15°，translateX ±50px，700ms）
- **卡片悬停**：上浮 4px + 阴影扩散（350ms）
- **按钮点击**：缩放 0.98×（300ms）

### 文案风格

- **标题**：简洁动词式，如"登录 星链 AI Hub"、"运行配置脚本"
- **正文**：逐步指引 + FAQ，避免技术术语，多用"看到 XX 就成功了"
- **代码块**：单行命令 + 一键复制按钮

## 更新记录

- **2026-06-18**：初版发布
  - 全新设计，替换旧教程
  - 品牌名从 Sub2API 更新为星链 AI Hub
  - 新增 Windows 支持，macOS/Windows 自动识别
  - 改用交互式脚本流程（不再提供带密钥的免交互命令）
  - 图片全部转 WebP 格式，体积减少 95%
  - 新增 iOS 风格 3D 翻转动画
  - 新增 Codex APP 使用说明
- **2026-06-20**：更新为自动创建 API Key 流程
  - 普通用户只需注册并登录 星链 AI Hub
  - 一键脚本通过浏览器授权自动创建 API Key 并写入配置
  - 更新 macOS / Windows 终端截图和登录授权示意图
  - 教程强调 Codex APP 增强模式，保留官方插件、手机远程操作等功能
- **2026-07-13**：补充 Codex 桌面端首次登录处理
  - 说明脚本完成后仍出现「登录 ChatGPT」页面并不代表配置失败
  - 增加「使用其他方式登录」和复制脚本自动创建的 `codex` API Key 操作步骤
  - 增加对应页面截图与常见问题说明
- **2026-07-14**：更新 CC Switch 配置流程
  - 替换为新版星链 AI Hub 和 CC Switch 操作截图
  - 优先引导使用「导入到 CCS」一键导入
  - 增加一键导入失败后的 Codex 供应商手动配置步骤
