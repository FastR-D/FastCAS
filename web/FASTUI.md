# FastUI console

`@fastrd/ui-react` 与直接使用的 `@fastrd/ui-foundation` 固定消费 GitHub release `v0.1.0`，锁文件保留制品完整性。安装后 `pnpm run build` 将 CSS/JS 打入本地控制台输出，运行时不访问包下载站点。

console 的加载和成功/错误反馈使用真实 `Loading` / `Alert` React 组件，邀请/恢复表单使用 `TextField` / `Button`；安全表单、导航、管理表格保留业务结构，以 foundation token 替换原绿色色板。`fastcas-theme` 默认为 system；切换系统外观不会重挂载 React 树。业务请求、凭证、CSRF 与管理员权限仍由现有组件管理。
