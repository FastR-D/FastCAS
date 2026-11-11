import { Save, X } from "lucide-react";
import { Icon } from "./Icon";
import { useState } from "react";
import { Alert, Button, Dialog, TextField, TextArea } from "@fastrd/ui-react";

export type Application = {
  id: string;
  name: string;
  redirect_uris: string[];
  grant_types: string[];
  scopes: string[];
  resources: string[];
  public: boolean;
  events_uri?: string;
  backchannel_logout_uri?: string;
};
export type ApplicationEdit = {
  application: Application;
  kind: "capabilities" | "callbacks";
};
const words = (value: string) => value.trim().split(/\s+/).filter(Boolean);

export function ApplicationEditor({
  edit,
  busy,
  api,
  run,
  onClose,
  onSaved,
}: {
  edit: ApplicationEdit;
  busy: boolean;
  api: <T>(path: string, body?: unknown) => Promise<T>;
  run: (work: () => Promise<void>) => Promise<void>;
  onClose: () => void;
  onSaved: (application: Application) => void;
}) {
  const app = edit.application;
  const [grants, setGrants] = useState(app.grant_types?.join(" ") ?? "");
  const [scopes, setScopes] = useState(app.scopes?.join(" ") ?? "");
  const [resources, setResources] = useState(app.resources?.join(" ") ?? "");
  const [events, setEvents] = useState(app.events_uri ?? "");
  const [logout, setLogout] = useState(app.backchannel_logout_uri ?? "");
  const [error, setError] = useState("");
  const capabilities = edit.kind === "capabilities";
  const title = capabilities ? "配置应用权限" : "配置通知地址";
  return (
    <Dialog open label={title} onClose={onClose} dismissible={!busy}>
      <div className="dialog-content">
        <h2>{title}</h2>
        <p>
          {app.name} · <code>{app.id}</code>
        </p>
        {error && <Alert tone="danger">{error}</Alert>}
        <form
          onSubmit={(event) => {
            event.preventDefault();
            setError("");
            void run(async () => {
              try {
                const changes = capabilities
                  ? {
                      grant_types: words(grants),
                      scopes: words(scopes),
                      resources: words(resources),
                    }
                  : {
                      events_uri: events.trim(),
                      backchannel_logout_uri: logout.trim(),
                    };
                await api(
                  "/admin/applications/" +
                    encodeURIComponent(app.id) +
                    "/" +
                    edit.kind,
                  changes,
                );
                onSaved({ ...app, ...changes });
              } catch (error) {
                setError(
                  error instanceof Error
                    ? error.message
                    : "保存失败，请稍后重试",
                );
              }
            });
          }}
        >
          <fieldset disabled={busy}>
            {capabilities ? (
              <>
                <p>
                  权限变更会影响应用可申请的授权类型、Scope
                  和资源受众。多个值用空格分隔。
                </p>
                <label>
                  授权类型
                  <TextArea
                    required
                    value={grants}
                    onChange={(event) => setGrants(event.target.value)}
                    spellCheck={false}
                  />
                </label>
                <label>
                  允许的 Scope
                  <TextField
                    value={scopes}
                    onChange={(event) => setScopes(event.target.value)}
                    spellCheck={false}
                  />
                </label>
                <label>
                  资源受众
                  <TextField
                    value={resources}
                    onChange={(event) => setResources(event.target.value)}
                    spellCheck={false}
                  />
                </label>
              </>
            ) : (
              <>
                <p>
                  请填写接收方的完整 HTTPS 地址。清空地址会移除对应通知配置。
                </p>
                <label>
                  绑定事件接收地址
                  <TextField
                    type="url"
                    value={events}
                    onChange={(event) => setEvents(event.target.value)}
                    placeholder="https://app.example/api/auth/fastcas/events"
                  />
                </label>
                <label>
                  退出通知接收地址
                  <TextField
                    type="url"
                    value={logout}
                    onChange={(event) => setLogout(event.target.value)}
                    placeholder="https://app.example/api/auth/fastcas/backchannel-logout"
                  />
                </label>
              </>
            )}
          </fieldset>
          <div className="dialog-actions">
            <Button disabled={busy} onClick={onClose}>
              <Icon icon={X} />
              取消
            </Button>
            <Button type="submit" variant="primary" loading={busy}>
              <Icon icon={Save} />
              保存配置
            </Button>
          </div>
        </form>
      </div>
    </Dialog>
  );
}
