import { createContext, useCallback, useMemo, useState } from "react";
import HelpPanel from "./HelpPanel";

export type HelpState = {
  open: boolean;
  slug: string | null;
  anchor: string | null;
};

export type HelpContextValue = {
  state: HelpState;
  openHelp: (slug: string, anchor?: string) => void;
  closeHelp: () => void;
};

export const HelpContext = createContext<HelpContextValue | null>(null);

export function HelpProvider({ children }: { children: React.ReactNode }) {
  const [state, setState] = useState<HelpState>({
    open: false,
    slug: null,
    anchor: null,
  });

  const openHelp = useCallback((slug: string, anchor?: string) => {
    setState({ open: true, slug, anchor: anchor ?? null });
  }, []);

  const closeHelp = useCallback(() => {
    setState((s) => ({ ...s, open: false }));
  }, []);

  const value = useMemo<HelpContextValue>(
    () => ({ state, openHelp, closeHelp }),
    [state, openHelp, closeHelp],
  );

  return (
    <HelpContext.Provider value={value}>
      {children}
      <HelpPanel />
    </HelpContext.Provider>
  );
}
