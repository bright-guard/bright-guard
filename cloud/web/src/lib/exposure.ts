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

// PDS Core semantic colors (see cloud/web/docs/csp-integration.md §3):
//   #006128 success / #a97f13 warning / #c05700 critical / #b71c1c alert.
// public=red(alert), cloud_internal=orange-amber(critical),
// unreachable=yellow(warning), internal=green(success), unknown=neutral slate.
export const EXPOSURE_BADGE_CLASS: Record<ExposureState, string> = {
  public: "bg-[#b71c1c]/10 text-[#b71c1c] ring-1 ring-[#b71c1c]/30",
  cloud_internal: "bg-[#c05700]/10 text-[#c05700] ring-1 ring-[#c05700]/30",
  internal: "bg-[#006128]/10 text-[#006128] ring-1 ring-[#006128]/30",
  unreachable: "bg-[#a97f13]/10 text-[#a97f13] ring-1 ring-[#a97f13]/30",
  unknown: "bg-slate-100 text-slate-600 ring-1 ring-slate-300",
};

// Solid fills for stacked bars and dot legends (anywhere the pill chrome
// would wash out at small sizes).
export const EXPOSURE_DOT_CLASS: Record<ExposureState, string> = {
  public: "bg-[#b71c1c]",
  cloud_internal: "bg-[#c05700]",
  internal: "bg-[#006128]",
  unreachable: "bg-[#a97f13]",
  unknown: "bg-slate-400",
};
