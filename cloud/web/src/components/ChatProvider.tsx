import {
  createContext,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { useAuth } from "../auth/AuthContext";
import {
  createChatSession,
  deleteChatSession,
  getChatSession,
  postChatMessage,
} from "../api/chat";
import { ApiError } from "../api/client";
import type {
  ChatBudgetStatus,
  ChatThreadMessage,
  ChatToolCallTrace,
} from "../api/types";
import ChatPanel from "./ChatPanel";
import ChatButton from "./ChatButton";

const SESSION_KEY = "bg.chat.sessionId";

export type ChatUiMessage = {
  id: string;
  role: "user" | "assistant";
  text: string;
  toolCalls?: ChatToolCallTrace[];
  pending?: boolean;
};

export type ChatErrorKind = "budget" | "upstream" | "unknown";

export type ChatError = {
  kind: ChatErrorKind;
  message: string;
  resetAt?: string;
};

type ChatContextValue = {
  open: boolean;
  openChat: () => void;
  closeChat: () => void;
  toggleChat: () => void;
  sessionId: string | null;
  messages: ChatUiMessage[];
  sending: boolean;
  error: ChatError | null;
  usage: ChatBudgetStatus | null;
  send: (text: string) => Promise<void>;
  newConversation: () => Promise<void>;
};

export const ChatContext = createContext<ChatContextValue | null>(null);

// Pull a plain-text rendering out of stored content parts, ignoring
// functionCall / functionResponse parts. Used when restoring an existing
// conversation.
function blocksToText(content: unknown): string {
  if (!Array.isArray(content)) return "";
  const out: string[] = [];
  for (const p of content as Array<{ text?: string }>) {
    if (p && typeof p.text === "string" && p.text) out.push(p.text);
  }
  return out.join("\n\n");
}

function threadToUiMessages(msgs: ChatThreadMessage[]): ChatUiMessage[] {
  const out: ChatUiMessage[] = [];
  for (const m of msgs) {
    if (m.role === "user") {
      const text = blocksToText(m.content);
      // Skip user turns that are pure tool_result echoes (no text blocks).
      if (text) out.push({ id: m.id, role: "user", text });
    } else if (m.role === "assistant") {
      const text = blocksToText(m.content);
      // Skip empty assistant turns (pure tool_use); the final one carries
      // the user-visible text.
      if (text) {
        out.push({
          id: m.id,
          role: "assistant",
          text,
          toolCalls: m.toolCalls,
        });
      }
    }
  }
  return out;
}

export function ChatProvider({ children }: { children: ReactNode }) {
  const { activeOrgId } = useAuth();
  const [open, setOpen] = useState(false);
  const [sessionId, setSessionId] = useState<string | null>(null);
  const [messages, setMessages] = useState<ChatUiMessage[]>([]);
  const [sending, setSending] = useState(false);
  const [error, setError] = useState<ChatError | null>(null);
  const [usage, setUsage] = useState<ChatBudgetStatus | null>(null);

  // Track which (org,session) tuple we've hydrated to avoid double-fetches.
  const hydrated = useRef<{ orgId: string; sessionId: string } | null>(null);

  // Restore the persisted session id once on mount.
  useEffect(() => {
    try {
      const sid = window.localStorage.getItem(SESSION_KEY);
      if (sid) setSessionId(sid);
    } catch {
      /* localStorage unavailable; fine */
    }
  }, []);

  // When org or sessionId changes, hydrate the thread from the server.
  useEffect(() => {
    if (!activeOrgId || !sessionId) return;
    const k = { orgId: activeOrgId, sessionId };
    if (
      hydrated.current &&
      hydrated.current.orgId === k.orgId &&
      hydrated.current.sessionId === k.sessionId
    ) {
      return;
    }
    hydrated.current = k;
    getChatSession(activeOrgId, sessionId)
      .then((t) => setMessages(threadToUiMessages(t.messages)))
      .catch(() => {
        // Stale id from another org or deleted session — clear.
        try {
          window.localStorage.removeItem(SESSION_KEY);
        } catch {
          /* noop */
        }
        setSessionId(null);
        setMessages([]);
      });
  }, [activeOrgId, sessionId]);

  // When org changes, discard the conversation: the persisted sessionId
  // belongs to one org and would 404 in another.
  useEffect(() => {
    setMessages([]);
    setError(null);
    setSessionId((sid) => {
      if (!sid) return sid;
      try {
        window.localStorage.removeItem(SESSION_KEY);
      } catch {
        /* noop */
      }
      return null;
    });
    // Intentionally only respond to activeOrgId.
  }, [activeOrgId]);

  const ensureSession = useCallback(async (): Promise<string | null> => {
    if (!activeOrgId) return null;
    if (sessionId) return sessionId;
    const s = await createChatSession(activeOrgId);
    setSessionId(s.id);
    try {
      window.localStorage.setItem(SESSION_KEY, s.id);
    } catch {
      /* noop */
    }
    return s.id;
  }, [activeOrgId, sessionId]);

  const send = useCallback(
    async (text: string) => {
      if (!activeOrgId) return;
      const t = text.trim();
      if (!t) return;
      setError(null);
      setSending(true);
      const userMsg: ChatUiMessage = {
        id: `tmp-${Date.now()}`,
        role: "user",
        text: t,
      };
      const pendingMsg: ChatUiMessage = {
        id: `tmp-pending-${Date.now()}`,
        role: "assistant",
        text: "",
        pending: true,
      };
      setMessages((prev) => [...prev, userMsg, pendingMsg]);
      try {
        const sid = await ensureSession();
        if (!sid) throw new Error("no session");
        const resp = await postChatMessage(activeOrgId, sid, t);
        setUsage(resp.usage);
        setMessages((prev) => {
          // Strip the pending placeholder, append the real assistant turn.
          const trimmed = prev.filter((m) => !m.pending);
          return [
            ...trimmed,
            {
              id: `srv-${Date.now()}`,
              role: "assistant",
              text: resp.assistant,
              toolCalls: resp.toolCalls,
            },
          ];
        });
      } catch (err) {
        let kind: ChatErrorKind = "unknown";
        let message = "Couldn't reach the assistant.";
        let resetAt: string | undefined;
        if (err instanceof ApiError) {
          if (err.status === 429) {
            kind = "budget";
            const body = err.body as { resetAt?: string; error?: string };
            message = body?.error ?? "daily token budget exceeded";
            resetAt = body?.resetAt;
          } else if (err.status >= 500) {
            kind = "upstream";
          }
        }
        setError({ kind, message, resetAt });
        // Drop pending placeholder so the user can retry.
        setMessages((prev) => prev.filter((m) => !m.pending));
      } finally {
        setSending(false);
      }
    },
    [activeOrgId, ensureSession],
  );

  const newConversation = useCallback(async () => {
    if (sessionId && activeOrgId) {
      // Fire-and-forget; if it fails the stale session is harmless.
      deleteChatSession(activeOrgId, sessionId).catch(() => undefined);
    }
    setMessages([]);
    setError(null);
    setSessionId(null);
    try {
      window.localStorage.removeItem(SESSION_KEY);
    } catch {
      /* noop */
    }
  }, [sessionId, activeOrgId]);

  const value = useMemo<ChatContextValue>(
    () => ({
      open,
      openChat: () => setOpen(true),
      closeChat: () => setOpen(false),
      toggleChat: () => setOpen((v) => !v),
      sessionId,
      messages,
      sending,
      error,
      usage,
      send,
      newConversation,
    }),
    [open, sessionId, messages, sending, error, usage, send, newConversation],
  );

  return (
    <ChatContext.Provider value={value}>
      {children}
      <ChatButton />
      <ChatPanel />
    </ChatContext.Provider>
  );
}
