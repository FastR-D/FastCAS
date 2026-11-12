import {
  EyeOff,
  Link2,
  Fingerprint,
  KeyRound,
  RefreshCw,
  ShieldCheck,
  Smartphone,
} from "lucide-react";
import { Icon } from "./Icon";
import { useState } from "react";
import { Button, TextField, TextArea, Badge } from "@fastrd/ui-react";
import { useConfirm } from "./Confirm";
import type { Me } from "./types";
import { QRCodeSVG } from "qrcode.react";

export function Security({
  me,
  api,
  busy,
  run,
  refresh,
  onNotice,
  onPasswordChanged,
}: {
  me: Me;
  api: <T>(path: string, body?: unknown) => Promise<T>;
  busy: boolean;
  run: (work: () => Promise<void>) => Promise<void>;
  refresh: () => Promise<void>;
  onNotice: (message: string) => void;
  onPasswordChanged: () => void;
}) {
  const confirm = useConfirm();
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const [uri, setUri] = useState("");
  const [enrollmentCode, setEnrollmentCode] = useState("");
  const [recovery, setRecovery] = useState<string[]>([]);
  const [currentPassword, setCurrentPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [changeCode, setChangeCode] = useState("");
  return (
    <>
      <div className="security-summary">
        <div>
          <span>当前身份</span>
          <strong>{me.user.name}</strong>
          <small>{me.user.role === "admin" ? "管理员" : "成员"}</small>
        </div>
        <div>
          <span>双重验证</span>
          <Badge tone={me.mfa_enabled ? "success" : "warning"}>
            {me.mfa_enabled ? "已启用" : "建议启用"}
          </Badge>
          <small>保护登录与敏感操作</small>
        </div>
        <div>
          <span>账号归属</span>
          <strong>项目独立管理</strong>
          <small>认证关联不合并账号与内容</small>
        </div>
      </div>
      <div className="grid security-grid">
        <div className="security-column">
          <section>
            <h2 className="icon-heading">
              <Icon icon={Fingerprint} size={18} />
              身份验证
            </h2>
            <p>{me.user.email}</p>
            <p>双重验证：{me.mfa_enabled ? "已启用" : "未启用"}</p>
            {me.password_enabled ? (
              <form
                onSubmit={(e) => {
                  e.preventDefault();
                  void run(async () => {
                    await api("/me/reauthenticate", { password, code });
                    setPassword("");
                    setCode("");
                    onNotice("身份已重新验证");
                    await refresh();
                  });
                }}
              >
                <label>
                  当前密码
                  <TextField
                    type="password"
                    autoComplete="current-password"
                    required
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                  />
                </label>
                <label>
                  动态验证码或恢复码
                  <TextField
                    autoComplete="one-time-code"
                    value={code}
                    onChange={(e) => setCode(e.target.value)}
                  />
                </label>
                <Button type="submit" variant="primary" disabled={busy}>
                  <Icon icon={Fingerprint} />
                  重新验证身份
                </Button>
              </form>
            ) : (
              <p>
                此账号通过 GitHub 登录。需要重新验证身份时，请
                <a href="/login">重新使用 GitHub 登录</a>。
              </p>
            )}
          </section>
          <section>
            <h2 className="icon-heading">
              <Icon icon={KeyRound} size={18} />
              {me.password_enabled ? "修改 FastCAS 密码" : "设置 FastCAS 密码"}
            </h2>
            <p>
              修改后会退出所有 FastCAS 会话和已关联项目中的 FastCAS
              登录。本地项目密码不变。
            </p>
            {!me.password_enabled && (
              <p>请在 GitHub 登录后五分钟内设置密码。设置后也可使用邮箱与密码登录。</p>
            )}
            <form
              onSubmit={(e) => {
                e.preventDefault();
                void run(async () => {
                  if (newPassword !== confirmPassword)
                    throw new Error("两次输入的新密码不一致");
                  await api("/me/password", {
                    current_password: currentPassword,
                    new_password: newPassword,
                    code: changeCode,
                  });
                  setCurrentPassword("");
                  setNewPassword("");
                  setConfirmPassword("");
                  setChangeCode("");
                  onPasswordChanged();
                  onNotice("FastCAS 密码已更新，请使用新密码重新登录");
                });
              }}
            >
              {me.password_enabled && (
                <label>
                  原 FastCAS 密码
                  <TextField
                    type="password"
                    autoComplete="current-password"
                    required
                    value={currentPassword}
                    onChange={(e) => setCurrentPassword(e.target.value)}
                  />
                </label>
              )}
              <label>
                新 FastCAS 密码
                <TextField
                  type="password"
                  autoComplete="new-password"
                  minLength={12}
                  required
                  value={newPassword}
                  onChange={(e) => setNewPassword(e.target.value)}
                />
              </label>
              <label>
                确认 FastCAS 新密码
                <TextField
                  type="password"
                  autoComplete="new-password"
                  required
                  value={confirmPassword}
                  onChange={(e) => setConfirmPassword(e.target.value)}
                />
              </label>
              {me.mfa_enabled && (
                <label>
                  改密动态验证码或恢复码
                  <TextField
                    autoComplete="one-time-code"
                    required
                    value={changeCode}
                    onChange={(e) => setChangeCode(e.target.value)}
                  />
                </label>
              )}
              <Button type="submit" variant="primary" disabled={busy}>
                <Icon icon={KeyRound} />
                {me.password_enabled ? "修改 FastCAS 密码" : "设置 FastCAS 密码"}
              </Button>
            </form>
          </section>
        </div>
        <div className="security-column">
          {(me.github_enabled || me.github_account) && (
            <section>
              <h2 className="icon-heading">
                <Icon icon={Link2} size={18} />
                GitHub 登录
              </h2>
              {me.github_account ? (
                <p>已绑定 GitHub：{me.github_account.login}</p>
              ) : (
                <>
                  <p>
                    绑定后可使用 GitHub 登录当前 FastCAS
                    账号。绑定前请先重新验证身份；已开启双重验证时也需验证动态验证码。
                  </p>
                  <Button
                    disabled={busy}
                    onClick={() =>
                      void run(async () => {
                        const result = await api<{ authorization_url: string }>(
                          "/me/github", {},
                        );
                        window.location.assign(result.authorization_url);
                      })
                    }
                  >
                    <Icon icon={Link2} />
                    绑定 GitHub
                  </Button>
                </>
              )}
            </section>
          )}
          <section>
            <h2 className="icon-heading">
              <Icon icon={ShieldCheck} size={18} />
              双重验证
            </h2>
            <p>使用认证器生成动态验证码。开启后请妥善保存一次性恢复码。</p>
            {me.mfa_enabled && (
              <>
                <p>
                  更新恢复码前，请先重新登录或重新验证身份与动态验证码；旧恢复码会立即失效。
                </p>
                <Button
                  disabled={busy}
                  onClick={async () => {
                    if (await confirm("生成新的恢复码并使所有旧恢复码失效？"))
                      void run(async () => {
                        const result = await api<{
                          recovery_codes: string[];
                        }>("/me/mfa/recovery-codes", {});
                        setRecovery(result.recovery_codes);
                        onNotice("恢复码已更新，旧码已失效");
                      });
                  }}
                >
                  <Icon icon={RefreshCw} />
                  更新恢复码
                </Button>
              </>
            )}
            {!me.mfa_enabled && !uri && (
              <Button
                disabled={busy}
                onClick={() =>
                  void run(async () =>
                    setUri(
                      (await api<{ otpauth_uri: string }>("/me/mfa/enroll", {}))
                        .otpauth_uri,
                    ),
                  )
                }
              >
                <Icon icon={Smartphone} />
                设置认证器
              </Button>
            )}
            {uri && (
              <>
                <p>使用认证器扫描二维码，或手动导入下方 URI。此密钥只应由你保存。</p>
                <div className="authenticator-qr">
                  <QRCodeSVG
                    value={uri}
                    size={224}
                    marginSize={4}
                    title="认证器配置二维码"
                    role="img"
                    aria-label="认证器配置二维码"
                  />
                </div>
                <TextArea
                  className="secret-value"
                  aria-label="认证器配置"
                  readOnly
                  value={uri}
                />
                <label>
                  认证器动态验证码
                  <TextField
                    autoComplete="one-time-code"
                    inputMode="numeric"
                    value={enrollmentCode}
                    onChange={(e) => setEnrollmentCode(e.target.value)}
                  />
                </label>
                <Button
                  disabled={busy || !enrollmentCode}
                  onClick={() =>
                    void run(async () => {
                      const result = await api<{
                        recovery_codes: string[];
                      }>("/me/mfa/confirm", { code: enrollmentCode });
                      setRecovery(result.recovery_codes);
                      setUri("");
                      setEnrollmentCode("");
                      await refresh();
                    })
                  }
                >
                  <Icon icon={ShieldCheck} />
                  确认验证码并开启
                </Button>
              </>
            )}
            {recovery.length > 0 && (
              <>
                <p>请现在保存恢复码，离开页面后不再显示。</p>
                <pre className="secret-value">{recovery.join("\n")}</pre>
                <Button onClick={() => setRecovery([])}>
                  <Icon icon={EyeOff} />
                  已保存，隐藏恢复码
                </Button>
              </>
            )}
          </section>
        </div>
      </div>
    </>
  );
}
