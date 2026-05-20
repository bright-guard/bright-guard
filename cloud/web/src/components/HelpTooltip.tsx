import { useEffect, useRef, useState } from "react";
import { TERMS } from "../lib/terms";
import { useHelp } from "../hooks/useHelp";

const HOVER_DELAY_MS = 200;

export default function HelpTooltip({
  term,
  children,
}: {
  term: string;
  children: React.ReactNode;
}) {
  const entry = TERMS[term];
  const [open, setOpen] = useState(false);
  const timerRef = useRef<number | null>(null);
  const wrapRef = useRef<HTMLSpanElement | null>(null);
  const { openHelp } = useHelp();

  // Close on outside click for touch users who tapped to open.
  useEffect(() => {
    if (!open) return;
    const onDoc = (e: MouseEvent) => {
      if (wrapRef.current && !wrapRef.current.contains(e.target as Node)) {
        setOpen(false);
      }
    };
    document.addEventListener("mousedown", onDoc);
    return () => document.removeEventListener("mousedown", onDoc);
  }, [open]);

  if (!entry) {
    return <>{children}</>;
  }

  function schedule(opening: boolean) {
    if (timerRef.current !== null) {
      window.clearTimeout(timerRef.current);
      timerRef.current = null;
    }
    timerRef.current = window.setTimeout(() => {
      setOpen(opening);
      timerRef.current = null;
    }, HOVER_DELAY_MS);
  }

  return (
    <span
      ref={wrapRef}
      className="relative inline-block"
      onMouseEnter={() => schedule(true)}
      onMouseLeave={() => schedule(false)}
      onFocus={() => setOpen(true)}
      onBlur={() => setOpen(false)}
    >
      <span
        tabIndex={0}
        onClick={(e) => {
          e.stopPropagation();
          setOpen((v) => !v);
        }}
        className="cursor-help underline decoration-dotted decoration-slate-400 underline-offset-2"
      >
        {children}
      </span>
      {open && (
        <span
          role="tooltip"
          className="absolute left-1/2 top-full z-30 mt-2 w-72 -translate-x-1/2 rounded-md border border-slate-200 bg-white p-3 text-left text-xs text-slate-700 shadow-lg"
        >
          <span className="block text-[11px] font-semibold uppercase tracking-wide text-slate-500">
            {entry.label}
          </span>
          <span className="mt-1 block leading-snug text-slate-700">
            {entry.definition}
          </span>
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              setOpen(false);
              openHelp(entry.slug, entry.anchor);
            }}
            className="mt-2 inline-block text-[var(--accent)] hover:underline"
          >
            Learn more →
          </button>
        </span>
      )}
    </span>
  );
}
