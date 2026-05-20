import { useHelp } from "../hooks/useHelp";

export default function PageHelp({
  slug,
  anchor,
  title,
}: {
  slug: string;
  anchor?: string;
  title?: string;
}) {
  const { openHelp } = useHelp();
  return (
    <button
      type="button"
      onClick={() => openHelp(slug, anchor)}
      title={title ?? "Help"}
      aria-label={title ?? "Open help"}
      className="inline-grid h-7 w-7 place-items-center rounded-full border border-slate-300 bg-white text-slate-400 transition hover:border-slate-500 hover:text-slate-700"
    >
      <span aria-hidden className="text-[13px] font-semibold leading-none">?</span>
    </button>
  );
}
