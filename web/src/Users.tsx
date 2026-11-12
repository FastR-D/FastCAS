import { useEffect, useState } from "react";
import { Alert, Badge, Button, Dialog, EmptyState, Select, TextField } from "@fastrd/ui-react";
import { ChevronDown, CirclePause, CirclePlay, Pencil, RotateCw, Save, Trash2, X } from "lucide-react";
import { Icon } from "./Icon";
import { useConfirm } from "./Confirm";

type User = {
  id: string;
  name: string;
  email: string;
  role: "member" | "admin";
  status: "active" | "disabled";
  email_verified: boolean;
  mfa_enabled: boolean;
  created_at: string;
};
type API = <T>(path: string, body?: unknown) => Promise<T>;
type Run = (work: () => Promise<void>) => Promise<void>;

function UserEditor({ user, current, api, busy, run, onClose, onSaved }: {
  user: User;
  current: boolean;
  api: API;
  busy: boolean;
  run: Run;
  onClose: () => void;
  onSaved: (user: User) => void;
}) {
  const [name, setName] = useState(user.name);
  const [email, setEmail] = useState(user.email);
  const [role, setRole] = useState(user.role);
  const [error, setError] = useState("");
  return (
    <Dialog open label="编辑用户" onClose={onClose} dismissible={!busy}>
      <div className="dialog-content">
        <h2>编辑用户</h2>
        <p>{user.email}</p>
        {error && <Alert tone="danger">{error}</Alert>}
        <form onSubmit={(event) => {
          event.preventDefault();
          setError("");
          void run(async () => {
            try {
              const updated = await api<User>("/admin/identities/" + encodeURIComponent(user.id), {
                name: name.trim(), email: email.trim(), role,
              });
              onSaved(updated);
            } catch (error) {
              setError(error instanceof Error ? error.message : "保存失败，请稍后重试");
            }
          });
        }}>
          <fieldset disabled={busy}>
            <label>姓名
              <TextField required maxLength={200} autoComplete="off" value={name} onChange={(event) => setName(event.target.value)} />
            </label>
            <label>邮箱
              <TextField type="email" required maxLength={254} autoComplete="off" value={email} onChange={(event) => setEmail(event.target.value)} />
            </label>
            <label>角色
              <Select aria-label="角色" value={role} disabled={current} onChange={(event) => setRole(event.target.value as User["role"])}>
                <option value="member">成员</option>
                <option value="admin">管理员</option>
              </Select>
            </label>
            {current && <p>当前账号的角色不可在此修改。</p>}
            <p>修改邮箱或角色后，该用户需要重新登录。</p>
          </fieldset>
          <div className="dialog-actions">
            <Button disabled={busy} onClick={onClose}><Icon icon={X} />取消</Button>
            <Button type="submit" variant="primary" loading={busy}><Icon icon={Save} />保存用户</Button>
          </div>
        </form>
      </div>
    </Dialog>
  );
}

export function Users({ currentUserId, api, busy, run, onCurrentUserUpdated }: {
  currentUserId: string;
  api: API;
  busy: boolean;
  run: Run;
  onCurrentUserUpdated: (user: User) => void;
}) {
  const confirm = useConfirm();
  const [users, setUsers] = useState<User[]>([]);
  const [cursor, setCursor] = useState("");
  const [loading, setLoading] = useState(true);
  const [editing, setEditing] = useState<User | null>(null);
  const [feedback, setFeedback] = useState("");
  async function load(after = "") {
    try {
      const items = await api<User[]>("/admin/identities" + (after ? "?after=" + encodeURIComponent(after) : ""));
      setUsers((previous) => after ? [...previous, ...items] : items);
      setCursor(items.length === 100 ? items[items.length - 1].id : "");
    } finally {
      setLoading(false);
    }
  }
  useEffect(() => { void run(() => load()); }, []);
  return (
    <>
      {feedback && <Alert tone="success">{feedback}</Alert>}
      {editing && <UserEditor
        key={editing.id}
        user={editing}
        current={editing.id === currentUserId}
        api={api}
        busy={busy}
        run={run}
        onClose={() => setEditing(null)}
        onSaved={(updated) => {
          setUsers((previous) => previous.map((user) => user.id === updated.id ? updated : user));
          setEditing(null);
          setFeedback("用户资料已更新");
          if (updated.id === currentUserId) onCurrentUserUpdated(updated);
        }}
      />}
      <section>
        <div className="section-heading">
          <div><h2>用户管理</h2><p>查看用户资料，管理角色与账号状态。</p></div>
          <Button disabled={busy} onClick={() => void run(() => load())}><Icon icon={RotateCw} />刷新用户</Button>
        </div>
        {!loading && !busy && users.length === 0 ? <EmptyState title="暂无用户" /> : (
          <div className="users-table-wrap">
            <table className="users-table" aria-label="用户列表" aria-busy={busy}>
              <thead><tr><th scope="col">用户</th><th scope="col">邮箱</th><th scope="col">角色</th><th scope="col">状态</th><th scope="col">双重验证</th><th scope="col">注册时间</th><th scope="col">操作</th></tr></thead>
              <tbody>{users.map((user) => {
                const current = user.id === currentUserId;
                return <tr key={user.id}>
                  <td><strong>{user.name}</strong>{current && <span className="current-user">当前账号</span>}<small title={user.id}>{user.id}</small></td>
                  <td>{user.email}<small>{user.email_verified ? "已验证" : "未验证"}</small></td>
                  <td>{user.role === "admin" ? "管理员" : "成员"}</td>
                  <td><Badge tone={user.status === "active" ? "success" : "warning"}>{user.status === "active" ? "正常" : "已停用"}</Badge></td>
                  <td>{user.mfa_enabled ? "已启用" : "未启用"}</td>
                  <td><time dateTime={user.created_at}>{new Date(user.created_at).toLocaleString()}</time></td>
                  <td><div className="user-actions">
                    <Button disabled={busy} onClick={() => { setFeedback(""); setEditing(user); }}><Icon icon={Pencil} />编辑</Button>
                    <Button disabled={busy || current} onClick={async () => {
                      const action = user.status === "active" ? "停用" : "启用";
                      if (await confirm(`确认${action}用户 ${user.email}？`)) void run(async () => {
                        const status = user.status === "active" ? "disabled" : "active";
                        await api("/admin/identities/" + encodeURIComponent(user.id) + "/status", { status });
                        setUsers((previous) => previous.map((item) => item.id === user.id ? { ...item, status } : item));
                        setFeedback(`用户已${action}`);
                      });
                    }}><Icon icon={user.status === "active" ? CirclePause : CirclePlay} />{user.status === "active" ? "停用" : "启用"}</Button>
                    <Button variant="danger" disabled={busy || current} onClick={async () => {
                      if (await confirm(`删除用户 ${user.email}？此操作无法恢复，将退出其 FastCAS 会话并撤销关联授权，项目本地账号和内容保留。`)) void run(async () => {
                        await api("/admin/identities/" + encodeURIComponent(user.id) + "/delete", {});
                        setUsers((previous) => previous.filter((item) => item.id !== user.id));
                        setFeedback("用户已删除");
                      });
                    }}><Icon icon={Trash2} />删除</Button>
                  </div></td>
                </tr>;
              })}</tbody>
            </table>
          </div>
        )}
        <div className="users-pagination"><small>已加载 {users.length} 位用户</small>{cursor && <Button disabled={busy} onClick={() => void run(() => load(cursor))}><Icon icon={ChevronDown} />加载更多用户</Button>}</div>
      </section>
    </>
  );
}
