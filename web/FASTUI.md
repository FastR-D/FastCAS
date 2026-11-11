# FastUI console

`@fastrd/ui-react` 与直接使用的 `@fastrd/ui-foundation` 精确固定消费官方 npm `0.1.2`，锁文件保留制品完整性。安装后 `pnpm run build` 将 CSS/JS 打入本地控制台输出，运行时不访问包下载站点。

保留 main 的桌面 Shell、Security、Admin、Audit、应用配置与安全确认对话框。基础表单实际使用 npm `TextField` / `TextArea` / `Select` / `Button`，加载和成功/错误反馈使用 `Loading` / `Alert`；宿主 CSS 只维护布局、密度及语义 token，没有复制浅深色板。

`src/theme-init.ts` 通过 Vite 编译为同源、parser-blocking 的 `assets/theme-init.js`，在页面内容前创建唯一 foundation controller。Shell 与原生 Auth/Device 的主题选择器复用 `window.fastCASColorMode`；保留 main 的 `fastcas.color-mode` key，默认为 system。切换主题不重挂载 React 树，不清除表单草稿/焦点。没有再次运行包的自动初始化脚本。

Auth/Device 页面继续使用 main 的 Vite manifest CSS 与原生 Go 表单校验，包括禁用 JavaScript 的登录；业务请求、一次性凭证、CSRF、近期 MFA、角色权限及确认取消行为由原有业务组件和服务端管理。
