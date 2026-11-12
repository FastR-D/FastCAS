import { ChevronDown, History, LogOut, RotateCw, Unlink } from "lucide-react";
import { Icon } from "./Icon";
import type { Me } from "./types";
import { useConfirm, ConfirmProvider } from "./Confirm";
import { Button } from "@fastrd/ui-react";
import React, { useEffect, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import "@fastrd/ui-react/styles.css";
import "./style.css";
import { Alert, EmptyState, Loading } from "@fastrd/ui-react";
import { Security } from "./Security";
import { Shell } from "./Shell";
import { Admin } from "./Admin";
import { Delegations } from "./Delegations";
import { Integration } from "./Integration";
type Link = {
  id: string;
  client_id: string;
  local_account_ref: string;
  state: string;
  version: number;
};
type Installation = {
  id: string;
  client_id: string;
  client_name: string;
  installation_id: string;
  created_at: string;
  revoked_at: string | null;
};
type Session = { id: string; authenticated_at: string; expires_at: string };
type Event = {
  id: string;
  client_id: string;
  attempts: number;
  last_status: number | null;
  dead_at: string | null;
};
type DeliveryAttempt = {
  id: number;
  attempt: number;
  outcome: string;
  http_status: number | null;
  duration_ms: number;
  finished_at: string;
};
function App() {
  const confirm = useConfirm();
  const [me, setMe] = useState<Me>();
  const [page, setPage] = useState("security");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    if (!me) {
      setPage("security");
      setLinks([]);
      setInstallations([]);
      setSessions([]);
      setEvents([]);
      setAttempts({});
    }
  }, [me?.session.id]);
  const [links, setLinks] = useState<Link[]>([]);
  const [installations, setInstallations] = useState<Installation[]>([]);
  const [installationCursor, setInstallationCursor] = useState("");
  const [sessions, setSessions] = useState<Session[]>([]);
  const [events, setEvents] = useState<Event[]>([]);
  const [cursor, setCursor] = useState("");
  const [attempts, setAttempts] = useState<Record<string, DeliveryAttempt[]>>(
    {},
  );
  async function api<T>(path: string, body?: unknown): Promise<T> {
    const response = await fetch("/api/v1" + path, {
      method: body === undefined ? "GET" : "POST",
      credentials: "same-origin",
      headers:
        body === undefined
          ? {}
          : {
              "Content-Type": "application/json",
              "X-CSRF-Token": me?.csrf ?? "",
            },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    if (!response.ok) {
      const problem = await response.json().catch(() => ({}));
      throw new Error(
        response.status === 401
          ? "请先登录或重新验证身份"
          : problem.message || "操作未完成，请检查权限与身份验证状态",
      );
    }
    return response.status === 204 ? undefined : response.json();
  }
  async function refresh() {
    try {
      setMe(await api<Me>("/me"));
    } catch (error) {
      if (error instanceof Error && error.message === "请先登录或重新验证身份") {
        window.location.replace("/login" + window.location.search);
      }
      throw error;
    }
  }
  const wasAuthenticated = useRef(false);
  useEffect(() => {
    if (me) wasAuthenticated.current = true;
    else if (wasAuthenticated.current) window.location.replace("/login");
  }, [me]);
  useEffect(() => {
    refresh()
      .catch((e) => {
        if (e.message !== "请先登录或重新验证身份") setError(e.message);
      })
      .finally(() => setLoading(false));
  }, []);
  const pendingRequests = useRef(0);
  async function run(work: () => Promise<void>) {
    pendingRequests.current += 1;
    setBusy(true);
    setError("");
    setNotice("");
    try {
      await work();
    } catch (e) {
      setError(e instanceof Error ? e.message : "请求失败");
    } finally {
      pendingRequests.current -= 1;
      setBusy(pendingRequests.current > 0);
    }
  }
  async function select(next: string) {
    setPage(next);
    await run(async () => {
      if (next === "links") setLinks(await api<Link[]>("/me/links"));
      if (next === "installations") await loadInstallations();
      if (next === "sessions")
        setSessions(await api<Session[]>("/me/sessions"));
      if (next === "outbox") await loadEvents();
    });
  }
  async function loadInstallations(before = "") {
    const data = await api<{
      installations: Installation[];
      next_cursor: string;
    }>(
      "/me/device-installations" +
        (before ? "?before=" + encodeURIComponent(before) : ""),
    );
    setInstallations(
      before ? (old) => [...old, ...data.installations] : data.installations,
    );
    setInstallationCursor(data.next_cursor);
  }
  async function loadEvents(before = "") {
    const data = await api<{ events: Event[]; next_cursor: string }>(
      "/admin/outbox" + (before ? "?before=" + encodeURIComponent(before) : ""),
    );
    setEvents(before ? (old) => [...old, ...data.events] : data.events);
    setCursor(data.next_cursor);
  }
  return (
    <Shell
      user={me?.user}
      page={page}
      busy={busy}
      onSelect={(next) => void select(next)}
      onLogout={() =>
        void run(async () => {
          await api("/logout", {});
          setMe(undefined);
          setNotice("已退出 FastCAS");
        })
      }
    >
      <div className="intro">
        <p className="eyebrow">FAST R&D · IDENTITY</p>
        <h1>{me ? `你好，${me.user.name}` : "你的科研账号安全中心"}</h1>
        <p>
          管理关联身份与登录安全。各项目的本地账号、内容和权限由项目独立管理。
        </p>
      </div>
      {busy && (
        <div className="pending">
          <Loading label="正在处理，请稍候…" />
        </div>
      )}
      {error && <Alert tone="danger">{error}</Alert>}
      {notice && <Alert tone="success">{notice}</Alert>}
      {loading ? (
        <Loading label="正在读取账号…" />
      ) : !me ? (
        <Loading label="正在前往登录…" />
      ) : (
        <>
          {page === "security" && (
            <Security
              me={me}
              api={api}
              busy={busy}
              run={run}
              refresh={refresh}
              onNotice={setNotice}
              onPasswordChanged={() => window.location.replace("/login?notice=password-changed")}
            />
          )}
          {page === "links" && (
            <section>
              <h2>关联项目</h2>
              <p>
                解除认证会终止相应 FastCAS 登录授权，保留项目本地账号与内容。
              </p>
              {!busy && !links.length && (
                <EmptyState title="暂无关联项目">
                  <p>请在项目的账号设置中发起认证。</p>
                </EmptyState>
              )}
              {links.map((link) => (
                <article key={link.id}>
                  <div>
                    <strong>{link.client_id}</strong>
                    <p>
                      {link.local_account_ref} · {link.state}
                    </p>
                  </div>
                  {link.state !== "revoked" && (
                    <Button
                      disabled={busy}
                      onClick={async () => {
                        if (await confirm("解除这个项目的 FastCAS 认证？"))
                          void run(async () => {
                            await api(
                              "/me/links/" +
                                encodeURIComponent(link.id) +
                                "/revoke",
                              { version: link.version },
                            );
                            setLinks(await api<Link[]>("/me/links"));
                          });
                      }}
                    >
                      <Icon icon={Unlink} />
                      解除认证
                    </Button>
                  )}
                </article>
              ))}
            </section>
          )}
          {page === "installations" && (
            <section>
              <h2>本机安装</h2>
              <p>
                管理通过设备码关联的本机安装。撤销后，本机下次联网检查会清除
                FastCAS 关联；本地任务不受影响。
              </p>
              {!busy && !installations.length && (
                <EmptyState title="暂无已关联安装">
                  <p>在本机应用中使用设备码关联 FastCAS。</p>
                </EmptyState>
              )}
              {installations.map((item) => (
                <article key={item.id}>
                  <div>
                    <strong>{item.client_name || item.client_id}</strong>
                    <p>安装 ID：{item.installation_id}</p>
                    <small>
                      {item.revoked_at
                        ? "已撤销 · " +
                          new Date(item.revoked_at).toLocaleString()
                        : "关联于 " +
                          new Date(item.created_at).toLocaleString()}
                    </small>
                  </div>
                  {!item.revoked_at && (
                    <Button
                      disabled={busy}
                      onClick={async () => {
                        if (await confirm("撤销这个本机安装的 FastCAS 关联？"))
                          void run(async () => {
                            await api(
                              "/me/device-installations/" +
                                encodeURIComponent(item.id) +
                                "/revoke",
                              {},
                            );
                            setInstallations((old) =>
                              old.map((current) =>
                                current.id === item.id
                                  ? {
                                      ...current,
                                      revoked_at: new Date().toISOString(),
                                    }
                                  : current,
                              ),
                            );
                            setNotice("已撤销安装关联");
                          });
                      }}
                    >
                      <Icon icon={Unlink} />
                      撤销关联
                    </Button>
                  )}
                </article>
              ))}
              {installationCursor && (
                <Button
                  disabled={busy}
                  onClick={() =>
                    void run(() => loadInstallations(installationCursor))
                  }
                >
                  <Icon icon={ChevronDown} />
                  加载更多安装
                </Button>
              )}
            </section>
          )}
          {page === "delegations" && (
            <Delegations key="member" api={api} busy={busy} run={run} />
          )}
          {page === "integration" && me.user.role === "admin" && (
            <Integration api={api} busy={busy} run={run} />
          )}
          {page === "exchange-policies" && me.user.role === "admin" && (
            <Delegations key="admin" api={api} busy={busy} run={run} admin />
          )}
          {page === "sessions" && (
            <section>
              <h2>登录会话</h2>
              <p>
                退出所有 FastCAS 会话将通知已关联项目终止使用 FastCAS
                登录的会话；项目本地密码登录不受影响。
              </p>
              <Button
                disabled={busy}
                onClick={async () => {
                  if (await confirm("退出所有 FastCAS 会话？"))
                    void run(async () => {
                      await api("/me/logout-all", {});
                      setMe(undefined);
                      setSessions([]);
                      setNotice("已退出所有 FastCAS 会话");
                    });
                }}
              >
                <Icon icon={LogOut} />
                退出所有 FastCAS 会话
              </Button>
              {sessions.map((session) => (
                <article key={session.id}>
                  <div>
                    <strong>
                      {session.id === me.session.id ? "当前会话" : "其他会话"}
                    </strong>
                    <p>
                      登录于{" "}
                      {new Date(session.authenticated_at).toLocaleString()}
                    </p>
                    <small>
                      到期于 {new Date(session.expires_at).toLocaleString()}
                    </small>
                  </div>
                  <Button
                    disabled={busy}
                    onClick={() =>
                      void run(async () => {
                        await api(
                          "/me/sessions/" +
                            encodeURIComponent(session.id) +
                            "/revoke",
                          {},
                        );
                        if (session.id === me.session.id) setMe(undefined);
                        else setSessions(await api<Session[]>("/me/sessions"));
                      })
                    }
                  >
                    <Icon icon={LogOut} />
                    退出会话
                  </Button>
                </article>
              ))}
            </section>
          )}
          {me.user.role === "admin" &&
            [
              "identities",
              "applications",
              "services",
              "credentials",
              "audit",
            ].includes(page) && (
              <Admin key={page} page={page} api={api} busy={busy} run={run} />
            )}
          {page === "outbox" && me.user.role === "admin" && (
            <section>
              <h2>失败投递</h2>
              <p>
                需要管理员近期双重验证。请先修复接收方故障，再重新排队。投递记录只显示状态与耗时，不显示消息内容。
              </p>
              {!busy && !events.length && (
                <EmptyState title="暂无失败投递">
                  <p>当前没有需要处理的死信。</p>
                </EmptyState>
              )}
              {events.map((event) => (
                <article key={event.id}>
                  <div>
                    <strong>{event.client_id}</strong>
                    <p>{event.id}</p>
                    <small>
                      已尝试 {event.attempts} 次 · HTTP{" "}
                      {event.last_status ?? "未知"}
                    </small>
                    <div>
                      <Button
                        disabled={busy}
                        onClick={() =>
                          void run(async () => {
                            const result = await api<{
                              attempts: DeliveryAttempt[];
                            }>(
                              "/admin/outbox/" +
                                encodeURIComponent(event.id) +
                                "/attempts",
                            );
                            setAttempts((old) => ({
                              ...old,
                              [event.id]: result.attempts,
                            }));
                          })
                        }
                      >
                        <Icon icon={History} />
                        查看投递记录
                      </Button>
                      {attempts[event.id] && (
                        <ul>
                          {attempts[event.id].map((attempt) => (
                            <li key={attempt.id}>
                              第 {attempt.attempt} 次 · {attempt.outcome} · HTTP{" "}
                              {attempt.http_status ?? "未收到"} ·{" "}
                              {attempt.duration_ms} 毫秒 ·{" "}
                              {new Date(attempt.finished_at).toLocaleString()}
                            </li>
                          ))}
                        </ul>
                      )}
                    </div>
                  </div>
                  {event.dead_at && (
                    <Button
                      disabled={busy}
                      onClick={() =>
                        void run(async () => {
                          await api(
                            "/admin/outbox/" +
                              encodeURIComponent(event.id) +
                              "/retry",
                            {},
                          );
                          await loadEvents();
                          setNotice("已重新排队");
                        })
                      }
                    >
                      <Icon icon={RotateCw} />
                      重试投递
                    </Button>
                  )}
                </article>
              ))}
              {cursor && (
                <Button
                  disabled={busy}
                  onClick={() => void run(() => loadEvents(cursor))}
                >
                  <Icon icon={ChevronDown} />
                  加载更多
                </Button>
              )}
            </section>
          )}
        </>
      )}
    </Shell>
  );
}
createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <ConfirmProvider>
      <App />
    </ConfirmProvider>
  </React.StrictMode>,
);
