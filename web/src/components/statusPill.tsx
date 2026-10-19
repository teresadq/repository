import { cn } from "@/lib/utils";

export type StatusTone = "neutral" | "info" | "warning" | "danger" | "purple" | "solid";

const TONES: Record<StatusTone, { pill: string; dot: string }> = {
	neutral: { pill: "border-border bg-muted/60 text-muted-foreground", dot: "bg-muted-foreground/60" },
	info: { pill: "border-chart-info/25 bg-chart-info/10 text-chart-info-ink", dot: "bg-chart-info" },
	warning: { pill: "border-chart-warning/30 bg-chart-warning/10 text-chart-warning-ink", dot: "bg-chart-warning" },
	danger: { pill: "border-destructive/25 bg-destructive/10 text-destructive", dot: "bg-destructive" },
	purple: { pill: "border-chart-cat-2/25 bg-chart-cat-2/10 text-chart-cat-2", dot: "bg-chart-cat-2" },
	solid: { pill: "border-transparent bg-foreground text-background", dot: "bg-background" },
};

export function StatusPill({
	tone = "neutral",
	children,
	dot = false,
	className,
}: {
	tone?: StatusTone;
	children: React.ReactNode;
	dot?: boolean;
	className?: string;
}) {
	const styles = TONES[tone];
	return (
		<span
			className={cn(
				"inline-flex w-fit items-center gap-1.5 rounded-full border px-1.5 py-0.5 text-[10px] leading-4 whitespace-nowrap",
				styles.pill,
				className,
			)}
		>
			{dot ? <span className={cn("size-1.5 rounded-full", styles.dot)} /> : null}
			{children}
		</span>
	);
}
