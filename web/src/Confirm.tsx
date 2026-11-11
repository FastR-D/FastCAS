import { Check } from "lucide-react";
import { Icon } from "./Icon";
import {
  createContext,
  useContext,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { Button, Dialog } from "@fastrd/ui-react";

const ConfirmContext = createContext<(message: string) => Promise<boolean>>(
  () => Promise.resolve(false),
);
export const useConfirm = () => useContext(ConfirmContext);

export function ConfirmProvider({ children }: { children: ReactNode }) {
  const [message, setMessage] = useState("");
  const pending = useRef<((accepted: boolean) => void) | null>(null);
  function finish(accepted: boolean) {
    pending.current?.(accepted);
    pending.current = null;
    setMessage("");
  }
  useEffect(
    () => () => {
      pending.current?.(false);
    },
    [],
  );
  return (
    <ConfirmContext.Provider
      value={(message) =>
        new Promise((resolve) => {
          pending.current?.(false);
          pending.current = resolve;
          setMessage(message);
        })
      }
    >
      {children}
      {message && (
        <Dialog
          open={Boolean(message)}
          label="确认操作"
          onClose={() => finish(false)}
        >
          <div className="dialog-content">
            <h2>确认操作</h2>
            <p>{message}</p>
            <div className="dialog-actions">
              <Button onClick={() => finish(false)}>取消</Button>
              <Button variant="danger" onClick={() => finish(true)}>
                <Icon icon={Check} />
                确认并继续
              </Button>
            </div>
          </div>
        </Dialog>
      )}
    </ConfirmContext.Provider>
  );
}
