import { type ReactNode } from "react";

type CalloutType = "tip" | "warning" | "info";

interface CalloutProps {
  type?: CalloutType;
  title?: string;
  children: ReactNode;
}

const typeConfig: Record<
  CalloutType,
  { border: string; bg: string; icon: string; titleColor: string }
> = {
  info: {
    border: "border-l-indigo-500",
    bg: "bg-indigo-500/5",
    icon: "ℹ",
    titleColor: "text-indigo-400",
  },
  tip: {
    border: "border-l-emerald-500",
    bg: "bg-emerald-500/5",
    icon: "✓",
    titleColor: "text-emerald-400",
  },
  warning: {
    border: "border-l-amber-500",
    bg: "bg-amber-500/5",
    icon: "⚠",
    titleColor: "text-amber-400",
  },
};

export default function Callout({
  type = "info",
  title,
  children,
}: CalloutProps) {
  const config = typeConfig[type];

  return (
    <div
      className={`border-l-4 ${config.border} ${config.bg} rounded-r-lg p-4 my-4`}
    >
      <div className="flex items-start gap-3">
        <span
          className={`${config.titleColor} font-bold text-base mt-0.5 select-none`}
          aria-hidden="true"
        >
          {config.icon}
        </span>
        <div className="flex-1 min-w-0">
          {title && (
            <p className={`font-semibold text-sm ${config.titleColor} mb-1`}>
              {title}
            </p>
          )}
          <div className="text-sm text-zinc-300 [&>p]:m-0">{children}</div>
        </div>
      </div>
    </div>
  );
}
