import { useAuth } from "../auth/AuthContext";
import { useChat } from "../hooks/useChat";

export default function ChatButton() {
  const { activeOrgId } = useAuth();
  const { open, openChat, sending } = useChat();
  // Hide on auth/onboarding pages (no org context to chat about) and while the
  // slide-over is open so the FAB doesn't sit on top of it.
  if (!activeOrgId || open) return null;
  return (
    <button
      type="button"
      onClick={openChat}
      aria-label="Open assistant"
      className="fixed bottom-6 right-6 z-40 grid h-12 w-12 place-items-center rounded-full text-white shadow-lg ring-1 ring-black/10 transition hover:shadow-xl"
      style={{ background: "var(--accent)" }}
    >
      <svg width="22" height="22" viewBox="0 0 24 24" fill="none" aria-hidden>
        <path
          d="M4 5h16v11H8l-4 4V5z"
          stroke="currentColor"
          strokeWidth="1.8"
          strokeLinejoin="round"
          fill="rgba(255,255,255,0.1)"
        />
        <circle cx="9" cy="10.5" r="1" fill="currentColor" />
        <circle cx="12" cy="10.5" r="1" fill="currentColor" />
        <circle cx="15" cy="10.5" r="1" fill="currentColor" />
      </svg>
      {sending && (
        <span className="absolute -top-1 -right-1 h-3 w-3 rounded-full bg-amber-400 ring-2 ring-white" />
      )}
    </button>
  );
}
