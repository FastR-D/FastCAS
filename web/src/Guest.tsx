import { KeyRound, Link, ShieldCheck, UserPlus } from "lucide-react";
import { Icon } from "./Icon";
import { Alert } from "@fastrd/ui-react";
import { Button, TextField } from "@fastrd/ui-react";
import { useState } from "react";

type Flow = "invite" | "claim" | "recovery" | "mfa_reset";
export type InitialCredential = { flow: Flow; token: string } | null;

// Remove one-time credentials from the address bar before React starts any requests.
export function takeInitialCredential(): InitialCredential {
  const query = new URLSearchParams(window.location.search);
  const flow = query.get("flow");
  const token = query.get("token");
  if (!flow && !token) return null;
  query.delete("flow");
  query.delete("token");
  const search = query.toString();
  window.history.replaceState(
    window.history.state,
    "",
    window.location.pathname + (search ? "?" + search : "") + window.location.hash,
  );
  return flow === "invite" || flow === "claim" || flow === "recovery" || flow === "mfa_reset"
    ? { flow, token: token ?? "" }
    : null;
}

export function Guest({ initial, onLogin }: {
  initial: InitialCredential;
  onLogin: () => void;
}) {
  const [flow, setFlow] = useState<Flow>(initial?.flow ?? "invite");
  const [token, setToken] = useState(initial?.token ?? "");
  const [name, setName] = useState("");
  const [password, setPassword] = useState("");
  const [confirmation, setConfirmation] = useState("");
  const [busy, setBusy] = useState(false);
  const [complete, setComplete] = useState(false);
  const [error, setError] = useState("");
  function selectFlow(next: Flow) {
    setFlow(next);
    setToken("");
    setPassword("");
    setConfirmation("");
    setError("");
  }
  async function redeem(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError("");
    if (flow !== "mfa_reset" && password !== confirmation) {
      setError("两次输入的密码不一致");
      return;
    }
    setBusy(true);
    try {
      const response = await fetch(
        "/api/v1/" +
          (flow === "invite"
            ? "register"
            : flow === "recovery"
              ? "recover"
                : flow === "claim"
                  ? "claim"
                  : "recover-mfa"),
        {
          method: "POST",
          credentials: "same-origin",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(
            flow === "mfa_reset"
              ? { token, password }
              : { token, name: flow === "invite" || flow === "claim" ? name : "", password },
          ),
        },
      );
      if (!response.ok) {
        setError(
          response.status === 429
            ? "尝试次数过多，请稍后再试"
            : flow === "claim"
              ? response.status === 409
                ? "登录邮箱已被占用，请联系管理员重新签发邀请码"
                : "请核对原账户姓名及邀请码。邀请码可能已使用或过期"
              : "凭证无效、已使用或已过期",
        );
        return;
      }
      setComplete(true);
      setToken("");
      setPassword("");
      setConfirmation("");
    } catch {
      setError("网络不可用，请稍后重试");
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="registration-flow">
      <h2>
        {flow === "invite"
          ? "接受邀请"
          : flow === "recovery"
            ? "恢复 FastCAS 密码"
            : flow === "claim"
              ? "绑定已有 FastResearch 账号"
              : "重置 FastCAS 双重验证"}
      </h2>
      {complete ? (
        <>
          <Alert tone="success">
            {flow === "invite"
              ? "账号已创建"
              : flow === "recovery"
                ? "密码已更新"
                : flow === "claim"
                  ? "账号已绑定，请使用管理员提供的登录邮箱和新密码登录"
                  : "双重验证已重置"}
            {flow === "claim" ? "。" : "。现在可以登录 FastCAS。"}
          </Alert>
          <a
            href="/login"
            onClick={(event) => {
              event.preventDefault();
              onLogin();
            }}
          >
            前往登录
          </a>
        </>
      ) : (
        <>
          <div className="flow-choices">
            <Button
              type="button"
              disabled={busy}
              aria-current={flow === "invite" ? "page" : undefined}
              onClick={() => {
                selectFlow("invite");
              }}
            >
              <Icon icon={UserPlus} />
              邀请注册
            </Button>
            <Button
              type="button"
              disabled={busy}
              aria-current={flow === "claim" ? "page" : undefined}
              onClick={() => selectFlow("claim")}
            >
              <Icon icon={Link} />
              绑定已有账号
            </Button>
            <Button
              type="button"
              disabled={busy}
              aria-current={flow === "recovery" ? "page" : undefined}
              onClick={() => {
                selectFlow("recovery");
              }}
            >
              <Icon icon={KeyRound} />
              密码恢复
            </Button>
            <Button
              type="button"
              disabled={busy}
              aria-current={flow === "mfa_reset" ? "page" : undefined}
              onClick={() => {
                selectFlow("mfa_reset");
              }}
            >
              <Icon icon={ShieldCheck} />
              双重验证重置
            </Button>
          </div>
            <form onSubmit={(e) => void redeem(e)}>
              <label>
                {flow === "claim" ? "邀请码" : "一次性凭证"}
                <TextField
                  autoComplete="off"
                  required
                  value={token}
                  onChange={(e) => setToken(e.target.value)}
                />
              </label>
              {(flow === "invite" || flow === "claim") && (
                <label>
                  {flow === "claim" ? "原 FastResearch 账户姓名" : "姓名"}
                  <TextField
                    autoComplete="name"
                    required
                    value={name}
                    onChange={(e) => setName(e.target.value)}
                  />
                </label>
              )}
              <label>
                {flow === "mfa_reset" ? "当前 FastCAS 密码" : "新密码"}
                <TextField
                  type="password"
                  autoComplete={
                    flow === "mfa_reset" ? "current-password" : "new-password"
                  }
                  required
                  minLength={12}
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                />
              </label>
              {flow !== "mfa_reset" && (
                <label>
                  确认新密码
                  <TextField
                    type="password"
                    autoComplete="new-password"
                    required
                    minLength={12}
                    value={confirmation}
                    onChange={(e) => setConfirmation(e.target.value)}
                  />
                </label>
              )}
              {error && <Alert tone="danger">{error}</Alert>}
              <Button type="submit" variant="primary" loading={busy}>
                {busy
                  ? "正在提交…"
                  : flow === "invite"
                    ? "创建 FastCAS 账号"
                    : flow === "recovery"
                      ? "更新 FastCAS 密码"
                      : flow === "claim"
                        ? "绑定 FastResearch 账号"
                        : "重置双重验证"}
              </Button>
            </form>
          </>
        )}
    </div>
  );
}
