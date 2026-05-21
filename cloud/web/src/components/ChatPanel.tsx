import { useEffect, useMemo, useRef, useState } from "react";
import { marked } from "marked";
import { useNavigate } from "react-router-dom";
import { useChat } from "../hooks/useChat";
import type { ChatUiMessage } from "./ChatProvider";

marked.setOptions({ gfm: true, breaks: true });

const EXAMPLES = [
  "Which MCP servers are publicly exposed?",
  "Who's the top caller in the last 7 days?",
  "Have I had any denials this week?",
  "Which capabilities have no policy coverage?",
];

export default function ChatPanel() {
  const {
    open,
    closeChat,
    messages,
    sending,
    error,
    usage,
    send,
    newConversation,
  } = useChat();
  const [draft, setDraft] = useState("");
  const scrollRef = useRef<HTMLDivElement | null>(null);
  const inputRef = useRef<HTMLTextAreaElement | null>(null);

  // Escape closes; body scroll lock matches HelpPanel's behavior.
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") closeChat();
    };
    window.addEventListener("keydown", onKey);
    const prev = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      window.removeEventListener("keydown", onKey);
      document.body.style.overflow = prev;
    };
  }, [open, closeChat]);

  // Scroll to bottom on new content.
  useEffect(() => {
    if (!open) return;
    const el = scrollRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [open, messages, sending]);

  // Auto-focus the input when the panel opens.
  useEffect(() => {
    if (open) inputRef.current?.focus();
  }, [open]);

  const onSend = () => {
    const t = draft.trim();
    if (!t || sending) return;
    setDraft("");
    void send(t);
  };

  const handleKey = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      onSend();
    }
  };

  return (
    <>
      <div
        aria-hidden={!open}
        onClick={closeChat}
        className={`fixed inset-0 z-40 bg-slate-900/30 transition-opacity duration-200 ${
          open ? "opacity-100" : "pointer-events-none opacity-0"
        }`}
      />
      <aside
        role="dialog"
        aria-modal="true"
        aria-label="Bright Guard assistant"
        className={`fixed inset-y-0 right-0 z-50 flex w-full max-w-[560px] flex-col bg-white shadow-2xl transition-transform duration-200 ease-out sm:w-[520px] ${
          open ? "translate-x-0" : "translate-x-full"
        }`}
      >
        <header className="flex items-start justify-between gap-3 border-b border-slate-200 bg-white px-5 py-3">
          <div className="min-w-0">
            <div className="text-[11px] uppercase tracking-wider text-slate-500">
              Assistant
            </div>
            <div className="truncate text-sm font-semibold text-slate-900">
              Bright Guard chat
            </div>
          </div>
          <div className="flex shrink-0 items-center gap-2">
            <button
              type="button"
              onClick={() => void newConversation()}
              className="rounded-md border border-slate-300 px-2 py-1 text-xs text-slate-700 hover:bg-slate-50"
            >
              New conversation
            </button>
            <button
              type="button"
              onClick={closeChat}
              aria-label="Close assistant"
              className="rounded-md p-1 text-slate-500 hover:bg-slate-100 hover:text-slate-900"
            >
              <svg width="18" height="18" viewBox="0 0 20 20" fill="none" aria-hidden>
                <path
                  d="M5 5l10 10M15 5L5 15"
                  stroke="currentColor"
                  strokeWidth="1.75"
                  strokeLinecap="round"
                />
              </svg>
            </button>
          </div>
        </header>

        <div ref={scrollRef} className="min-h-0 flex-1 overflow-y-auto px-5 py-4">
          {messages.length === 0 && !sending && (
            <EmptyState onPick={(s) => void send(s)} />
          )}
          <div className="flex flex-col gap-3">
            {messages.map((m) => (
              <MessageBubble key={m.id} m={m} />
            ))}
            {sending && <TypingDots />}
          </div>
        </div>

        {error && (
          <div
            className={`border-t px-5 py-2 text-[13px] ${
              error.kind === "budget"
                ? "border-amber-200 bg-amber-50 text-amber-800"
                : "border-rose-200 bg-rose-50 text-rose-800"
            }`}
          >
            {error.kind === "budget"
              ? `Daily token budget exceeded. Try again ${
                  error.resetAt ? "after " + new Date(error.resetAt).toLocaleString() : "tomorrow"
                }.`
              : error.message}
          </div>
        )}

        <footer className="border-t border-slate-200 bg-white px-5 py-3">
          <div className="flex items-end gap-2">
            <textarea
              ref={inputRef}
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              onKeyDown={handleKey}
              placeholder="Ask about your tenant…"
              rows={1}
              className="min-h-[36px] max-h-32 flex-1 resize-none rounded-md border border-slate-300 px-3 py-2 text-sm focus:border-[var(--accent)] focus:outline-none"
            />
            <button
              type="button"
              onClick={onSend}
              disabled={sending || draft.trim() === ""}
              className="rounded-md bg-[var(--accent)] px-3 py-2 text-sm font-semibold text-white shadow-sm transition disabled:cursor-not-allowed disabled:opacity-50"
            >
              Send
            </button>
          </div>
          <div className="mt-1 flex items-center justify-between text-[11px] text-slate-500">
            <span>Enter to send · Shift+Enter for newline</span>
            {usage && (
              <span>
                today: {usage.used.toLocaleString()}
                {usage.budget > 0 ? ` / ${usage.budget.toLocaleString()}` : ""} tokens
              </span>
            )}
          </div>
        </footer>
      </aside>
    </>
  );
}

function EmptyState({ onPick }: { onPick: (s: string) => void }) {
  return (
    <div className="flex flex-col gap-3 rounded-lg border border-dashed border-slate-300 bg-slate-50 p-4 text-sm text-slate-700">
      <div className="text-[13px] text-slate-600">
        Ask anything about your MCP servers, gateways, callers, activity, or policies.
        Try one of these:
      </div>
      <div className="flex flex-col gap-2">
        {EXAMPLES.map((s) => (
          <button
            key={s}
            type="button"
            onClick={() => onPick(s)}
            className="rounded-md border border-slate-200 bg-white px-3 py-2 text-left text-[13px] text-slate-800 hover:border-[var(--accent)] hover:text-[var(--accent)]"
          >
            {s}
          </button>
        ))}
      </div>
    </div>
  );
}

function MessageBubble({ m }: { m: ChatUiMessage }) {
  const navigate = useNavigate();
  const proseRef = useRef<HTMLDivElement | null>(null);

  // Internal /app/... links should route via react-router; external links
  // get target=_blank + rel=noopener. We use one delegated handler per
  // assistant bubble so the marked output stays plain HTML.
  useEffect(() => {
    const el = proseRef.current;
    if (!el) return;
    // Annotate external links once on mount / when html changes.
    el.querySelectorAll("a[href]").forEach((node) => {
      const a = node as HTMLAnchorElement;
      const href = a.getAttribute("href") || "";
      if (!href.startsWith("/")) {
        a.setAttribute("target", "_blank");
        a.setAttribute("rel", "noopener noreferrer");
      }
    });
    const onClick = (e: MouseEvent) => {
      const a = (e.target as HTMLElement | null)?.closest("a");
      if (!a) return;
      const href = a.getAttribute("href") || "";
      if (!href.startsWith("/")) return; // external — let the browser handle it
      if (e.metaKey || e.ctrlKey || e.shiftKey || e.altKey || e.button === 1) return;
      e.preventDefault();
      navigate(href);
    };
    el.addEventListener("click", onClick);
    return () => el.removeEventListener("click", onClick);
  }, [m.text, navigate]);

  if (m.role === "user") {
    return (
      <div className="flex justify-end">
        <div
          className="max-w-[85%] rounded-lg px-3 py-2 text-sm"
          style={{ background: "var(--accent-soft)", color: "#1f2937" }}
        >
          {m.text}
        </div>
      </div>
    );
  }
  const html = useMemo(
    () => (m.text ? (marked.parse(m.text, { async: false }) as string) : ""),
    [m.text],
  );
  return (
    <div className="flex justify-start">
      <div className="max-w-[92%] space-y-2">
        {m.toolCalls && m.toolCalls.length > 0 && (
          <ToolCallList calls={m.toolCalls} />
        )}
        {m.text && (
          <div
            ref={proseRef}
            className="docs-prose rounded-lg bg-slate-50 px-3 py-2 text-[14px]"
            dangerouslySetInnerHTML={{ __html: html }}
          />
        )}
      </div>
    </div>
  );
}

function ToolCallList({ calls }: { calls: NonNullable<ChatUiMessage["toolCalls"]> }) {
  const [openIdx, setOpenIdx] = useState<number | null>(null);
  return (
    <div className="flex flex-col gap-1">
      {calls.map((c, i) => {
        const isOpen = openIdx === i;
        return (
          <div
            key={i}
            className="rounded-md border border-slate-200 bg-white text-[12px] text-slate-700"
          >
            <button
              type="button"
              onClick={() => setOpenIdx(isOpen ? null : i)}
              className="flex w-full items-center justify-between px-2 py-1 text-left"
            >
              <span>
                Looked up: <code className="font-mono">{c.name}</code>
                {c.error ? (
                  <span className="ml-2 text-rose-600">· error</span>
                ) : (
                  <span className="ml-2 text-slate-400">
                    · {c.durationMs}ms
                  </span>
                )}
              </span>
              <span className="text-slate-400">{isOpen ? "−" : "+"}</span>
            </button>
            {isOpen && (
              <pre className="overflow-x-auto whitespace-pre-wrap break-words border-t border-slate-200 bg-slate-50 px-2 py-1 font-mono text-[11px] text-slate-800">
                {JSON.stringify(c.input, null, 2)}
                {c.error ? "\n\nerror: " + c.error : ""}
              </pre>
            )}
          </div>
        );
      })}
    </div>
  );
}

function TypingDots() {
  return (
    <div className="flex items-center gap-1 text-slate-400">
      <span className="dot" />
      <span className="dot" />
      <span className="dot" />
      <style>{`
        .dot { width:6px; height:6px; border-radius:9999px; background: currentColor; animation: bg-blink 1.2s infinite; }
        .dot:nth-child(2) { animation-delay: .2s; }
        .dot:nth-child(3) { animation-delay: .4s; }
        @keyframes bg-blink { 0%,80%,100% { opacity:.25 } 40% { opacity:1 } }
      `}</style>
    </div>
  );
}
