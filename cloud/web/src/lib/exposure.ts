import type { ExposureState } from "../api/types";

export const EXPOSURE_STATES: ExposureState[] = [
  "public",
  "cloud_internal",
  "internal",
  "unreachable",
  "unknown",
];

export const EXPOSURE_LABEL: Record<ExposureState, string> = {
  public: "Public",
  cloud_internal: "Cloud internal",
  internal: "Internal",
  unreachable: "Unreachable",
  unknown: "Unknown",
};

// Tailwind classes per state. Colors mandated by the spec:
// internal=slate, cloud_internal=blue, public=red, unknown=gray, unreachable=amber.
export const EXPOSURE_BADGE_CLASS: Record<ExposureState, string> = {
  public: "bg-rose-900/50 text-rose-300 ring-1 ring-rose-700/40",
  cloud_internal: "bg-blue-900/50 text-blue-200 ring-1 ring-blue-700/40",
  internal: "bg-slate-800 text-slate-200 ring-1 ring-slate-700",
  unreachable: "bg-amber-900/50 text-amber-200 ring-1 ring-amber-700/40",
  unknown: "bg-slate-900/60 text-slate-400 ring-1 ring-slate-700/60",
};
