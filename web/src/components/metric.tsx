import type { ReactNode } from "react";

import { cn } from "@/lib/utils";

export function Metric({
	label,
	value,
	hint,
	trend,
	tone = "default",
}: {
	label: string;
	value: ReactNode;
	hint?: ReactNode;
	trend?: ReactNode;
	tone?: "default" | "warning" | "danger";
}) {
	return (
		<div className="bg-card rounded-lg border p-4">
			<div className="flex items-start justify-between gap-2">
				<p className="text-muted-foreground text-xs">{label}</p>
				{trend ? <span className="text-muted-foreground text-[11px]">{trend}</span> : null}
			</div>
			<p
				className={cn(
					"mt-2 text-2xl font-semibold tracking-tight tabular-nums",
					tone === "warning" && "text-chart-warning-ink",
					tone === "danger" && "text-destructive",
				)}
			>
				{value}
			</p>
			{hint ? <p className="text-muted-foreground mt-1 text-[11px]">{hint}</p> : null}
		</div>
	);
}
