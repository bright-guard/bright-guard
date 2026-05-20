import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { api, ApiError } from "../api/client";

export type User = {
  id: string;
  email: string;
  displayName: string;
  avatarUrl: string;
  createdAt: string;
};

export type Org = {
  id: string;
  name: string;
  slug: string;
  createdBy: string;
  createdAt: string;
};

export type Membership = {
  org: Org;
  role: "owner" | "admin" | "member";
};

export type Me = {
  user: User;
  memberships: Membership[];
  activeOrgId: string | null;
  platformAdmin: boolean;
};

type AuthState = {
  loading: boolean;
  user: User | null;
  memberships: Membership[];
  activeOrgId: string | null;
  devLoginEnabled: boolean;
  platformAdmin: boolean;
  refresh: () => Promise<void>;
  logout: () => Promise<void>;
  setActiveOrg: (orgId: string) => Promise<void>;
};

const AuthCtx = createContext<AuthState | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [loading, setLoading] = useState(true);
  const [user, setUser] = useState<User | null>(null);
  const [memberships, setMemberships] = useState<Membership[]>([]);
  const [activeOrgId, setActiveOrgId] = useState<string | null>(null);
  const [devLoginEnabled, setDevLoginEnabled] = useState(false);
  const [platformAdmin, setPlatformAdmin] = useState(false);

  const refresh = useCallback(async () => {
    try {
      const me = await api<Me>("/api/me");
      setUser(me.user);
      setMemberships(me.memberships ?? []);
      setActiveOrgId(me.activeOrgId ?? null);
      setPlatformAdmin(!!me.platformAdmin);
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        setUser(null);
        setMemberships([]);
        setActiveOrgId(null);
        setPlatformAdmin(false);
      } else {
        // network error etc — log and clear
        console.error("refresh failed", err);
        setUser(null);
        setPlatformAdmin(false);
      }
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    api<{ enabled: boolean }>("/api/dev/enabled")
      .then((r) => setDevLoginEnabled(!!r.enabled))
      .catch(() => setDevLoginEnabled(false));
    refresh();
  }, [refresh]);

  const logout = useCallback(async () => {
    await api<void>("/auth/logout", { method: "POST" });
    setUser(null);
    setMemberships([]);
    setActiveOrgId(null);
    setPlatformAdmin(false);
  }, []);

  const setActiveOrg = useCallback(
    async (orgId: string) => {
      await api<void>("/api/sessions/active-org", {
        method: "POST",
        body: JSON.stringify({ orgId }),
      });
      setActiveOrgId(orgId);
    },
    [],
  );

  const value = useMemo<AuthState>(
    () => ({
      loading,
      user,
      memberships,
      activeOrgId,
      devLoginEnabled,
      platformAdmin,
      refresh,
      logout,
      setActiveOrg,
    }),
    [
      loading,
      user,
      memberships,
      activeOrgId,
      devLoginEnabled,
      platformAdmin,
      refresh,
      logout,
      setActiveOrg,
    ],
  );

  return <AuthCtx.Provider value={value}>{children}</AuthCtx.Provider>;
}

export function useAuth() {
  const ctx = useContext(AuthCtx);
  if (!ctx) throw new Error("useAuth must be used inside AuthProvider");
  return ctx;
}
