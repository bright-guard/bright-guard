import { useCallback, useEffect, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { api, ApiError } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import type { Invitation } from "../api/types";

export default function InvitationPage() {
  const { id } = useParams<{ id: string }>();
  const { refresh } = useAuth();
  const navigate = useNavigate();
  const [invite, setInvite] = useState<Invitation | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState<"accept" | "decline" | null>(null);

  const load = useCallback(async () => {
    if (!id) return;
    setLoading(true);
    setError(null);
    try {
      // /api/me/invitations returns ALL pending invites for the caller. We
      // pluck the one whose id matches the URL, so the page is meaningful
      // only when the caller's email matches the invite's email.
      const all = await api<Invitation[]>("/api/me/invitations");
      const match = all.find((i) => i.id === id) ?? null;
      setInvite(match);
      if (!match) {
        setError(
          "This invitation isn't addressed to you, or it has already been used or revoked.",
        );
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, [id]);

  useEffect(() => {
    load();
  }, [load]);

  async function accept() {
    if (!id) return;
    setBusy("accept");
    setError(null);
    try {
      await api<Invitation>(`/api/invitations/${id}/accept`, { method: "POST" });
      // Membership list & active org changed server-side; reload context.
      await refresh();
      navigate("/app");
    } catch (err) {
      if (err instanceof ApiError && typeof err.body === "string") {
        setError(err.body);
      } else {
        setError(err instanceof Error ? err.message : String(err));
      }
      setBusy(null);
    }
  }

  async function decline() {
    if (!id) return;
    setBusy("decline");
    setError(null);
    try {
      await api<void>(`/api/invitations/${id}/decline`, { method: "POST" });
      navigate("/app");
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
      setBusy(null);
    }
  }

  return (
    <div className="flex min-h-full items-center justify-center px-4 py-16">
      <div className="w-full max-w-lg rounded-2xl border border-slate-200 bg-white p-8 shadow-xl">
        <h1 className="mb-1 text-2xl font-semibold">Organization invitation</h1>
        {loading && (
          <p className="text-sm text-slate-500">Loading…</p>
        )}
        {!loading && invite && (
          <>
            <p className="mb-6 text-sm text-slate-500">
              <span className="text-slate-900">
                {invite.inviterName || invite.inviterEmail}
              </span>{" "}
              invited you to join{" "}
              <span className="text-slate-900">{invite.orgName}</span> as{" "}
              <span className="text-slate-900">{invite.role}</span>.
            </p>
            <div className="flex gap-3">
              <button
                onClick={accept}
                disabled={busy !== null}
                className="rounded-md bg-brand-500 px-4 py-2 text-sm font-medium text-white hover:bg-brand-400 disabled:opacity-50"
              >
                {busy === "accept" ? "Accepting…" : "Accept"}
              </button>
              <button
                onClick={decline}
                disabled={busy !== null}
                className="rounded-md border border-slate-300 px-4 py-2 text-sm text-slate-900 hover:bg-slate-100 disabled:opacity-50"
              >
                {busy === "decline" ? "Declining…" : "Decline"}
              </button>
            </div>
          </>
        )}
        {error && (
          <div className="mt-4 rounded-md border border-rose-300 bg-rose-50 px-3 py-2 text-sm text-rose-700">
            {error}
          </div>
        )}
      </div>
    </div>
  );
}
