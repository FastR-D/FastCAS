import { ChevronDown, ListFilter, RefreshCw, RotateCcw } from "lucide-react";
import { Icon } from "./Icon";
import { useEffect, useState } from "react";
import { Button, TextField, EmptyState } from "@fastrd/ui-react";

type Event = {
  id: number;
  actor: string;
  action: string;
  target: string;
  created_at: string;
  request_id: string;
  result: "unknown" | "success" | "failure";
};
type Filters = {
  action: string;
  actor: string;
  target: string;
  since: string;
  until: string;
};
const empty: Filters = {
  action: "",
  actor: "",
  target: "",
  since: "",
  until: "",
};

export function Audit({
  api,
  busy,
  run,
}: {
  api: <T>(path: string) => Promise<T>;
  busy: boolean;
  run: (work: () => Promise<void>) => Promise<void>;
}) {
  const [draft, setDraft] = useState(empty),
    [filters, setFilters] = useState(empty);
  const [events, setEvents] = useState<Event[]>([]),
    [cursor, setCursor] = useState("");
  async function load(selected: Filters, before = "") {
    const query = new URLSearchParams();
    for (const [key, value] of Object.entries(selected))
      if (value.trim())
        query.set(
          key,
          key === "since" || key === "until"
            ? new Date(value).toISOString()
            : value.trim(),
        );
    if (before) query.set("before", before);
    const data = await api<Event[]>("/admin/audit-events?" + query);
    setEvents((old) => (before ? [...old, ...data] : data));
    setCursor(data.length === 100 ? String(data[data.length - 1].id) : "");
    setFilters(selected);
  }
  useEffect(() => {
    void run(() => load(empty));
  }, []);
  return (
    <section>
      <div className="section-heading">
        <div>
          <h2>最近审计</h2>
          <p>按操作、执行者、目标和时间范围检索。时间以当前浏览器时区输入。</p>
        </div>
        <Button disabled={busy} onClick={() => void run(() => load(filters))}>
          <Icon icon={RefreshCw} />
          刷新记录
        </Button>
      </div>
      <form
        className="audit-filters"
        onSubmit={(event) => {
          event.preventDefault();
          void run(() => load(draft));
        }}
      >
        <fieldset disabled={busy}>
          <div className="filter-fields">
            <label>
              操作名称
              <TextField
                placeholder="例如 client.rotate_secret"
                value={draft.action}
                onChange={(event) =>
                  setDraft({ ...draft, action: event.target.value })
                }
                maxLength={200}
              />
            </label>
            <label>
              执行者 ID
              <TextField
                value={draft.actor}
                onChange={(event) =>
                  setDraft({ ...draft, actor: event.target.value })
                }
                maxLength={200}
              />
            </label>
            <label>
              目标 ID
              <TextField
                value={draft.target}
                onChange={(event) =>
                  setDraft({ ...draft, target: event.target.value })
                }
                maxLength={200}
              />
            </label>
            <label>
              开始时间
              <TextField
                type="datetime-local"
                value={draft.since}
                max={draft.until || undefined}
                onChange={(event) =>
                  setDraft({ ...draft, since: event.target.value })
                }
              />
            </label>
            <label>
              结束时间
              <TextField
                type="datetime-local"
                value={draft.until}
                min={draft.since || undefined}
                onChange={(event) =>
                  setDraft({ ...draft, until: event.target.value })
                }
              />
            </label>
          </div>
          <div className="filter-actions">
            <Button type="submit" variant="primary">
              <Icon icon={ListFilter} />
              筛选记录
            </Button>
            <Button
              onClick={() => {
                setDraft(empty);
                void run(() => load(empty));
              }}
            >
              <Icon icon={RotateCcw} />
              重置筛选
            </Button>
            <small>文本字段精确匹配 · 每次最多 100 条</small>
          </div>
        </fieldset>
      </form>
      {!busy && !events.length && (
        <EmptyState title="没有匹配的审计记录">
          <p>调整筛选条件或稍后刷新。</p>
        </EmptyState>
      )}
      <div className="audit-table-wrap">
        <table className="audit-table">
          <caption className="sr-only">
            审计事件，按事件编号从新到旧排列
          </caption>
          <thead>
            <tr>
              <th scope="col">操作 / 事件编号</th>
              <th scope="col">目标</th>
              <th scope="col">执行者</th>
              <th scope="col">结果 / 请求编号</th>
              <th scope="col">时间</th>
            </tr>
          </thead>
          <tbody>
            {events.map((event) => (
              <tr key={event.id}>
                <td>
                  <strong>{event.action}</strong>
                  <small>#{event.id}</small>
                </td>
                <td>{event.target || "—"}</td>
                <td>{event.actor || "—"}</td>
                <td><span>{event.result === "success" ? "成功" : event.result === "failure" ? "失败" : "历史记录"}</span><small>{event.request_id || "—"}</small></td>
                <td>
                  <time dateTime={event.created_at}>
                    {new Date(event.created_at).toLocaleString()}
                  </time>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {cursor && (
        <Button
          disabled={busy}
          onClick={() => void run(() => load(filters, cursor))}
        >
          <Icon icon={ChevronDown} />
          加载更多审计
        </Button>
      )}
    </section>
  );
}
