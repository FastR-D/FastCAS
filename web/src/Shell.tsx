import type { ReactNode } from "react";
import { Button, Badge } from "@fastrd/ui-react";
import {
  ShieldCheck,
  Link2,
  Laptop,
  Workflow,
  Monitor,
  Users,
  AppWindow,
  Bot,
  GitBranch,
  KeyRound,
  CodeXml,
  ScrollText,
  Send,
  LogOut,
  type LucideIcon,
} from "lucide-react";
import { Icon } from "./Icon";

const groups: { label: string; items: [string, string, LucideIcon][] }[] = [
  {
    label: "个人账号",
    items: [
      ["security", "安全设置", ShieldCheck],
      ["links", "关联项目", Link2],
      ["installations", "本机安装", Laptop],
      ["delegations", "跨项目委托", Workflow],
      ["sessions", "登录会话", Monitor],
    ],
  },
  {
    label: "组织管理",
    items: [
      ["identities", "用户管理", Users],
      ["applications", "应用管理", AppWindow],
      ["services", "服务身份", Bot],
      ["exchange-policies", "调用策略", GitBranch],
      ["credentials", "邀请与恢复", KeyRound],
    ],
  },
  {
    label: "开发与运维",
    items: [
      ["integration", "开发者接入", CodeXml],
      ["audit", "审计", ScrollText],
      ["outbox", "失败投递", Send],
    ],
  },
];

function Mark() {
  return <Icon icon={ShieldCheck} size={34} />;
}

export function Shell({
  user,
  page,
  busy,
  onSelect,
  onLogout,
  children,
}: {
  user?: { name: string; email: string; role: string };
  page: string;
  busy: boolean;
  onSelect: (page: string) => void;
  onLogout: () => void;
  children: ReactNode;
}) {
  const current =
    groups.flatMap((group) => group.items).find(([key]) => key === page)?.[1] ??
    "安全设置";
  return (
    <div className={`shell ${user ? "is-authenticated" : "is-guest"}`}>
      <a className="skip-link" href="#content">
        跳至主要内容
      </a>
      <aside className="sidebar">
        <a className="brand" href="/console/">
          <span className="brand-mark">
            <Mark />
          </span>
          <span>
            FastCAS<small>统一身份与访问</small>
          </span>
        </a>
        {user ? (
          <>
            <div className="workspace-label">
              身份控制台{" "}
              <Badge tone="info">
                {user.role === "admin" ? "管理员" : "成员"}
              </Badge>
            </div>
            <nav aria-label="账号导航">
              {groups
                .filter((_, index) => index === 0 || user.role === "admin")
                .map((group) => (
                  <div className="nav-group" key={group.label}>
                    <p>{group.label}</p>
                    {group.items.map(([key, label, icon]) => (
                      <Button
                        key={key}
                        variant="navigation"
                        aria-current={page === key ? "page" : undefined}
                        disabled={busy}
                        onClick={() => onSelect(key)}
                      >
                        <Icon icon={icon} className="nav-icon" />
                        {label}
                      </Button>
                    ))}
                  </div>
                ))}
            </nav>
          </>
        ) : (
          <div className="guest-aside">
            <Badge tone="info">FastR-D 账号服务</Badge>
            <h2>
              连接你的
              <br />
              科研工作空间
            </h2>
            <p>用一个可信身份连接各个项目，集中管理登录、授权与账号安全。</p>
            <div className="guest-principle">
              独立账号 · 显式关联 · 随时撤销
            </div>
          </div>
        )}
      </aside>
      <div className="workspace">
        <header className="topbar">
          <div className="breadcrumb">
            {user ? "控制台" : "账号服务"}
            <span>/</span>
            <strong>{user ? current : "欢迎使用 FastCAS"}</strong>
          </div>
          <div className="topbar-actions">
            {user && (
              <>
                <div className="account-summary">
                  <span className="avatar" aria-hidden="true">
                    {Array.from(user.name)[0]?.toUpperCase()}
                  </span>
                  <span>{user.name}</span>
                </div>
                <Button variant="quiet" disabled={busy} onClick={onLogout}>
                  <Icon icon={LogOut} />
                  退出
                </Button>
              </>
            )}
          </div>
        </header>
        <main id="content" tabIndex={-1}>
          {children}
        </main>
      </div>
    </div>
  );
}
