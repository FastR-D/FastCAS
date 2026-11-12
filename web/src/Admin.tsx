import {
  ChevronDown,
  CirclePause,
  CirclePlay,
  EyeOff,
  KeyRound,
  Plus,
  RotateCw,
  Settings2,
  Webhook,
} from "lucide-react";
import { Icon } from "./Icon";
import { useConfirm } from "./Confirm";
import { Alert, EmptyState } from "@fastrd/ui-react";
import {
  ApplicationEditor,
  type Application,
  type ApplicationEdit,
} from "./ApplicationEditor";
import { Audit as AuditPage } from "./Audit";
import {
  Button,
  TextField,
  TextArea,
  Select,
  Checkbox,
} from "@fastrd/ui-react";
import { useEffect, useState } from "react";
type API = <T>(path: string, body?: unknown) => Promise<T>;
type ServiceAccount = {
  client: Application;
  active: boolean;
  created_at: string;
};
export function Admin({
  page,
  api,
  busy,
  run,
}: {
  page: string;
  api: API;
  busy: boolean;
  run: (work: () => Promise<void>) => Promise<void>;
}) {
  const confirm = useConfirm();
  const [applications, setApplications] = useState<Application[]>([]),
    [services, setServices] = useState<ServiceAccount[]>([]);
  const [applicationCursor, setApplicationCursor] = useState(""),
    [serviceCursor, setServiceCursor] = useState("");
  const [kind, setKind] = useState<"invite" | "recovery" | "mfa_reset">(
      "invite",
    ),
    [email, setEmail] = useState(""),
    [issued, setIssued] = useState("");
  const [editing, setEditing] = useState<ApplicationEdit | null>(null),
    [feedback, setFeedback] = useState("");
  const [secretFor, setSecretFor] = useState("");
  const [id, setId] = useState(""),
    [name, setName] = useState(""),
    [redirect, setRedirect] = useState(""),
    [scopes, setScopes] = useState("openid profile email"),
    [clientKind, setClientKind] = useState<
      "web" | "public" | "device" | "service"
    >("web"),
    [resources, setResources] = useState(""),
    [exchangeEnabled, setExchangeEnabled] = useState(false),
    [eventsURI, setEventsURI] = useState(""),
    [logoutURI, setLogoutURI] = useState(""),
    [createdSecret, setCreatedSecret] = useState("");
  const [serviceID, setServiceID] = useState(""),
    [serviceName, setServiceName] = useState(""),
    [serviceScopes, setServiceScopes] = useState(""),
    [serviceResources, setServiceResources] = useState("");
  async function loadApplications(after = "") {
    const items = await api<Application[]>(
      "/admin/applications" +
        (after ? "?after=" + encodeURIComponent(after) : ""),
    );
    setApplications((old) => (after ? [...old, ...items] : items));
    setApplicationCursor(
      items.length === 100 ? items[items.length - 1].id : "",
    );
  }
  async function loadServices(after = "") {
    const items = await api<ServiceAccount[]>(
      "/admin/service-accounts" +
        (after ? "?after=" + encodeURIComponent(after) : ""),
    );
    setServices((old) => (after ? [...old, ...items] : items));
    setServiceCursor(
      items.length === 100 ? items[items.length - 1].client.id : "",
    );
  }
  useEffect(() => {
    void run(async () => {
      if (page === "applications") await loadApplications();
      if (page === "services") await loadServices();
    });
  }, [page]);
  if (page === "credentials")
    return (
      <section>
        <h2>邀请与恢复</h2>
        <p>
          一次性凭证只显示一次。请通过可信渠道交给账号持有人。双重验证重置仍要求持有人输入当前
          FastCAS 密码，成功后会退出其 FastCAS 会话。
        </p>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            void run(async () => {
              const result = await api<{ token: string }>(
                "/admin/credentials",
                { kind, email },
              );
              setIssued(result.token);
            });
          }}
        >
          <label>
            操作
            <Select
              value={kind}
              onChange={(e) => {
                setKind(e.target.value as "invite" | "recovery" | "mfa_reset");
                setIssued("");
              }}
            >
              <option value="invite">邀请注册</option>
              <option value="recovery">密码恢复</option>
              <option value="mfa_reset">双重验证重置</option>
            </Select>
          </label>
          <label>
            目标邮箱
            <TextField
              type="email"
              required
              value={email}
              onChange={(e) => setEmail(e.target.value)}
            />
          </label>
          <Button type="submit" disabled={busy}>
            <Icon icon={KeyRound} />
            生成一次性凭证
          </Button>
        </form>
        {issued && (
          <div>
            <p>请现在复制并私下交付，关闭后不再显示。</p>
            <TextArea
              className="secret-value"
              aria-label="一次性凭证"
              readOnly
              value={issued}
            />
            <Button onClick={() => setIssued("")}>已交付，隐藏</Button>
          </div>
        )}
      </section>
    );
  if (page === "services")
    return (
      <div className="grid">
        <section>
          <h2>服务身份</h2>
          {!busy && !services.length && <EmptyState title="暂无服务身份" />}
          <p>
            服务身份只能使用登记的 scope
            和资源受众。停用后旧密钥与中心令牌失效；重新启用会生成新密钥。
          </p>
          {services.map((item) => (
            <article key={item.client.id}>
              <div>
                <strong>{item.client.name}</strong>
                <p>
                  {item.client.id} · {item.active ? "启用" : "停用"}
                </p>
                <small>
                  {item.client.scopes.join(", ")} →{" "}
                  {item.client.resources.join(", ")}
                </small>
              </div>
              {item.active && (
                <Button
                  disabled={busy}
                  onClick={async () => {
                    if (await confirm(`停用 ${item.client.name}？`))
                      void run(async () => {
                        await api(
                          "/admin/service-accounts/" +
                            encodeURIComponent(item.client.id) +
                            "/status",
                          { active: false },
                        );
                        setServices((old) =>
                          old.map((entry) =>
                            entry.client.id === item.client.id
                              ? { ...entry, active: false }
                              : entry,
                          ),
                        );
                      });
                  }}
                >
                  <Icon icon={CirclePause} />
                  停用
                </Button>
              )}
              {!item.active && (
                <Button
                  disabled={busy}
                  onClick={() =>
                    void run(async () => {
                      const result = await api<{ client_secret: string }>(
                        "/admin/service-accounts/" +
                          encodeURIComponent(item.client.id) +
                          "/status",
                        { active: true },
                      );
                      setSecretFor(item.client.id);
                      setCreatedSecret(result.client_secret);
                      setServices((old) =>
                        old.map((entry) =>
                          entry.client.id === item.client.id
                            ? { ...entry, active: true }
                            : entry,
                        ),
                      );
                    })
                  }
                >
                  <Icon icon={CirclePlay} />
                  重新启用
                </Button>
              )}
              {item.active && (
                <Button
                  disabled={busy}
                  onClick={async () => {
                    if (await confirm(`轮换 ${item.client.name} 的密钥？`))
                      void run(async () => {
                        const result = await api<{ client_secret: string }>(
                          "/admin/service-accounts/" +
                            encodeURIComponent(item.client.id) +
                            "/rotate-secret",
                          {},
                        );
                        setSecretFor(item.client.id);
                        setCreatedSecret(result.client_secret);
                      });
                  }}
                >
                  <Icon icon={RotateCw} />
                  轮换密钥
                </Button>
              )}
            </article>
          ))}
          {serviceCursor && (
            <Button
              disabled={busy}
              onClick={() => void run(() => loadServices(serviceCursor))}
            >
              <Icon icon={ChevronDown} />
              加载更多服务身份
            </Button>
          )}
        </section>
        <section>
          <h2>登记服务身份</h2>
          <form
            onSubmit={(e) => {
              e.preventDefault();
              void run(async () => {
                const result = await api<{ client_secret: string }>(
                  "/admin/service-accounts",
                  {
                    id: serviceID,
                    name: serviceName,
                    scopes: serviceScopes.split(/\s+/).filter(Boolean),
                    resources: serviceResources.split(/\s+/).filter(Boolean),
                  },
                );
                setSecretFor(serviceID);
                setCreatedSecret(result.client_secret);
                setServices(
                  await api<ServiceAccount[]>("/admin/service-accounts"),
                );
              });
            }}
          >
            <label>
              服务 ID
              <TextField
                required
                value={serviceID}
                onChange={(e) => setServiceID(e.target.value)}
              />
            </label>
            <label>
              名称
              <TextField
                required
                value={serviceName}
                onChange={(e) => setServiceName(e.target.value)}
              />
            </label>
            <label>
              允许的 Scope
              <TextField
                required
                value={serviceScopes}
                onChange={(e) => setServiceScopes(e.target.value)}
              />
            </label>
            <label>
              资源受众
              <TextField
                required
                value={serviceResources}
                onChange={(e) => setServiceResources(e.target.value)}
              />
            </label>
            <Button type="submit" disabled={busy}>
              <Icon icon={Plus} />
              登记服务
            </Button>
          </form>
          {createdSecret && (
            <>
              <p>请现在保存 {secretFor} 的客户端密钥；离开后无法再次查看。</p>
              <TextArea
                className="secret-value"
                aria-label="服务客户端密钥"
                readOnly
                value={createdSecret}
              />
              <Button onClick={() => setCreatedSecret("")}>
                <Icon icon={EyeOff} />
                已保存，隐藏密钥
              </Button>
            </>
          )}
        </section>
      </div>
    );
  if (page === "applications")
    return (
      <>
        {feedback && <Alert tone="success">{feedback}</Alert>}
        {editing && (
          <ApplicationEditor
            edit={editing}
            busy={busy}
            api={api}
            run={run}
            onClose={() => setEditing(null)}
            onSaved={(updated) => {
              setApplications((old) =>
                old.map((item) => (item.id === updated.id ? updated : item)),
              );
              setEditing(null);
              setFeedback("应用配置已更新");
            }}
          />
        )}
        <div className="grid">
          <section>
            <h2>已登记应用</h2>
            {!busy && !applications.length && (
              <EmptyState title="暂无已登记应用">
                <p>登记应用后即可配置接入权限与通知。</p>
              </EmptyState>
            )}
            {applications.map((app) => (
              <article key={app.id}>
                <div>
                  <strong>{app.name}</strong>
                  <p>
                    {app.id} · {app.public ? "公开客户端" : "保密客户端"}
                  </p>
                  <small>
                    {app.grant_types?.join(", ")}{" "}
                    {app.redirect_uris?.join(", ")}{" "}
                    {app.events_uri && `事件 ${app.events_uri}`}{" "}
                    {app.backchannel_logout_uri &&
                      `退出 ${app.backchannel_logout_uri}`}
                  </small>
                </div>
                {!app.public && (
                  <Button
                    disabled={busy}
                    onClick={() => {
                      setFeedback("");
                      setEditing({ application: app, kind: "capabilities" });
                    }}
                  >
                    <Icon icon={Settings2} />
                    配置权限
                  </Button>
                )}
                {!app.public &&
                  app.grant_types?.includes("authorization_code") && (
                    <Button
                      disabled={busy}
                      onClick={() => {
                        setFeedback("");
                        setEditing({ application: app, kind: "callbacks" });
                      }}
                    >
                      <Icon icon={Webhook} />
                      配置通知地址
                    </Button>
                  )}
                {!app.public && (
                  <Button
                    disabled={busy}
                    onClick={async () => {
                      if (
                        await confirm(
                          `为 ${app.name} 轮换密钥？旧密钥将保留 10 分钟交接窗口。`,
                        )
                      )
                        void run(async () => {
                          const result = await api<{ client_secret: string }>(
                            "/admin/applications/" +
                              encodeURIComponent(app.id) +
                              "/rotate-secret",
                            {},
                          );
                          setSecretFor(app.id);
                          setCreatedSecret(result.client_secret);
                        });
                    }}
                  >
                    <Icon icon={RotateCw} />
                    轮换密钥
                  </Button>
                )}
              </article>
            ))}
            {applicationCursor && (
              <Button
                disabled={busy}
                onClick={() =>
                  void run(() => loadApplications(applicationCursor))
                }
              >
                <Icon icon={ChevronDown} />
                加载更多应用
              </Button>
            )}
          </section>
          <section>
            <h2>登记应用</h2>
            <p>
              Web
              应用须精确配置回调地址；设备应用使用公开客户端，服务身份须指定资源受众。保密客户端密钥仅显示一次。
            </p>
            <form
              onSubmit={(e) => {
                e.preventDefault();
                void run(async () => {
                  const result = await api<{ client_secret: string }>(
                    "/admin/applications",
                    {
                      id,
                      name,
                      redirect_uris: redirect
                        .split("\n")
                        .map((x) => x.trim())
                        .filter(Boolean),
                      scopes: scopes.split(/\s+/).filter(Boolean),
                      public:
                        clientKind === "public" || clientKind === "device",
                      grant_types:
                        clientKind === "device"
                          ? ["urn:ietf:params:oauth:grant-type:device_code"]
                          : clientKind === "service"
                            ? ["client_credentials"]
                            : [
                                "authorization_code",
                                "refresh_token",
                                ...(clientKind === "web" && exchangeEnabled
                                  ? [
                                      "urn:ietf:params:oauth:grant-type:token-exchange",
                                    ]
                                  : []),
                              ],
                      resources:
                        clientKind === "service" ||
                        (clientKind === "web" && exchangeEnabled)
                          ? resources.split(/\s+/).filter(Boolean)
                          : [],
                      events_uri:
                        clientKind === "web" ? eventsURI.trim() : undefined,
                      backchannel_logout_uri:
                        clientKind === "web" ? logoutURI.trim() : undefined,
                      development: false,
                    },
                  );
                  setSecretFor(id);
                  setCreatedSecret(result.client_secret);
                  setApplications(
                    await api<Application[]>("/admin/applications"),
                  );
                });
              }}
            >
              <label>
                应用 ID
                <TextField
                  required
                  value={id}
                  onChange={(e) => setId(e.target.value)}
                />
              </label>
              <label>
                名称
                <TextField
                  required
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                />
              </label>
              <label>
                客户端类型
                <Select
                  value={clientKind}
                  onChange={(e) =>
                    setClientKind(
                      e.target.value as "web" | "public" | "device" | "service",
                    )
                  }
                >
                  <option value="web">保密 Web 应用</option>
                  <option value="public">公开 Web 应用</option>
                  <option value="device">公开设备应用</option>
                  <option value="service">服务身份</option>
                </Select>
              </label>
              {(clientKind === "web" || clientKind === "public") && (
                <label>
                  回调地址（每行一个）
                  <TextArea
                    required
                    value={redirect}
                    onChange={(e) => setRedirect(e.target.value)}
                  />
                </label>
              )}
              <label>
                允许的 Scope
                <TextField
                  value={scopes}
                  onChange={(e) => setScopes(e.target.value)}
                />
              </label>
              {clientKind === "web" && (
                <>
                  <label>
                    绑定事件接收地址
                    <TextField
                      type="url"
                      value={eventsURI}
                      onChange={(e) => setEventsURI(e.target.value)}
                      placeholder="https://app.example/api/auth/fastcas/events"
                    />
                  </label>
                  <label>
                    退出通知接收地址
                    <TextField
                      type="url"
                      value={logoutURI}
                      onChange={(e) => setLogoutURI(e.target.value)}
                      placeholder="https://app.example/api/auth/fastcas/backchannel-logout"
                    />
                  </label>
                </>
              )}
              {clientKind === "web" && (
                <label>
                  <Checkbox
                    checked={exchangeEnabled}
                    onChange={(e) => setExchangeEnabled(e.target.checked)}
                  />
                  允许受限跨项目委托
                </label>
              )}
              {(clientKind === "service" ||
                (clientKind === "web" && exchangeEnabled)) && (
                <label>
                  资源受众（空格分隔）
                  <TextField
                    required
                    value={resources}
                    onChange={(e) => setResources(e.target.value)}
                  />
                </label>
              )}
              <Button type="submit" disabled={busy}>
                <Icon icon={Plus} />
                登记应用
              </Button>
            </form>
            {createdSecret && (
              <>
                <p>
                  请现在保存 {secretFor}{" "}
                  的客户端密钥。轮换后旧密钥只在交接窗口内有效。
                </p>
                <TextArea
                  className="secret-value"
                  aria-label="客户端密钥"
                  readOnly
                  value={createdSecret}
                />
                <Button onClick={() => setCreatedSecret("")}>
                  <Icon icon={EyeOff} />
                  已保存，隐藏
                </Button>
              </>
            )}
          </section>
        </div>
      </>
    );
  if (page === "audit") return <AuditPage api={api} busy={busy} run={run} />;
  return null;
}
