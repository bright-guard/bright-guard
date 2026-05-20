import { useContext } from "react";
import { HelpContext, type HelpContextValue } from "../components/HelpProvider";

export function useHelp(): HelpContextValue {
  const ctx = useContext(HelpContext);
  if (!ctx) {
    throw new Error("useHelp must be used inside a HelpProvider");
  }
  return ctx;
}
