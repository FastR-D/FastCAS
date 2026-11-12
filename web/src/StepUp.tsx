import { useEffect, useRef, useState } from "react";
import { Alert, Button, Dialog, TextField } from "@fastrd/ui-react";

export class VerificationCancelled extends Error {}

export function useStepUpVerification({
  enabled,
  sessionID,
  verify,
  onSetup,
}: {
  enabled: boolean;
  sessionID?: string;
  verify: (code: string) => Promise<void>;
  onSetup: () => void;
}) {
  const [open, setOpen] = useState(false);
  const [code, setCode] = useState("");
  const [error, setError] = useState("");
  const [verifying, setVerifying] = useState(false);
  const pending = useRef<{
    promise: Promise<void>;
    resolve: () => void;
    reject: (reason: Error) => void;
  } | null>(null);

  function finish(verified: boolean) {
    const request = pending.current;
    pending.current = null;
    setOpen(false);
    setCode("");
    setError("");
    setVerifying(false);
    if (verified) request?.resolve();
    else request?.reject(new VerificationCancelled());
  }
  useEffect(() => {
    setOpen(false);
    setCode("");
    setError("");
    setVerifying(false);
    return () => {
      pending.current?.reject(new VerificationCancelled());
      pending.current = null;
    };
  }, [sessionID]);

  function requireVerification() {
    // Concurrent protected requests share one prompt and one proof.
    if (pending.current) return pending.current.promise;
    let resolve!: () => void;
    let reject!: (reason: Error) => void;
    const promise = new Promise<void>((accept, cancel) => {
      resolve = accept;
      reject = cancel;
    });
    pending.current = { promise, resolve, reject };
    setCode("");
    setError("");
    setOpen(true);
    return promise;
  }

  const dialog = open ? (
    <Dialog open label="安全验证" onClose={() => finish(false)}>
      <div className="dialog-content">
        <h2>安全验证</h2>
        {enabled ? (
          <form
            onSubmit={(event) => {
              event.preventDefault();
              if (verifying || !code.trim()) return;
              const request = pending.current;
              setVerifying(true);
              setError("");
              void verify(code.trim())
                .then(() => {
                  if (pending.current === request) finish(true);
                })
                .catch((cause: unknown) => {
                  if (pending.current === request) {
                    setError(
                      cause instanceof Error
                        ? cause.message
                        : "验证未完成，请重试",
                    );
                    setCode("");
                  }
                })
                .finally(() => {
                  if (pending.current === request) setVerifying(false);
                });
            }}
          >
            <p>输入认证器验证码或一次性恢复码，验证后将继续刚才的操作。</p>
            <label>
              授权码
              <TextField
                autoFocus
                autoComplete="one-time-code"
                required
                maxLength={128}
                value={code}
                readOnly={verifying}
                onChange={(event) => setCode(event.target.value)}
              />
            </label>
            {error && <Alert tone="danger">{error}</Alert>}
            <div className="dialog-actions">
              <Button type="button" onClick={() => finish(false)}>
                取消
              </Button>
              <Button
                type="submit"
                variant="primary"
                disabled={verifying || !code.trim()}
              >
                {verifying ? "正在验证…" : "验证并继续"}
              </Button>
            </div>
          </form>
        ) : (
          <>
            <p>请先在安全验证中设置认证器，再继续此操作。</p>
            <p>
              如需更新登录状态，可<a href="/login">重新登录</a>后设置。
            </p>
            <div className="dialog-actions">
              <Button onClick={() => finish(false)}>取消</Button>
              <Button
                variant="primary"
                onClick={() => {
                  finish(false);
                  onSetup();
                }}
              >
                前往设置
              </Button>
            </div>
          </>
        )}
      </div>
    </Dialog>
  ) : null;
  return { requireVerification, dialog };
}
