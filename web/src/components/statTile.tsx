import { useId } from "react";

import { cn } from "@/lib/utils";

export function Sparkline({ points, tone = "accent" }: { points: number[]; tone?: "accent" | "muted" }) {
	const gradientId = `spark-${useId().replace(/[:]/g, "")}`;
	const width = 120;
	const height = 36;
	const values = points.length > 1 ? points : [0, 0];
	const min = Math.min(...values);
	const max = Math.max(...values);
	const span = max - min || 1;
	const step = width / (values.length - 1);
	const coords = values.map(
		(value, index) => [index * step, height - 4 - ((value - min) / span) * (height - 8)] as const,
	);
	const line = coords.map(([x, y]) => `${x.toFixed(1)},${y.toFixed(1)}`).join(" ");
	const area = `0,${height} ${line} ${width},${height}`;
	const stroke = tone === "accent" ? "var(--chart-cat-1)" : "var(--chart-neutral)";

	return (
		<svg viewBox={`0 0 ${width} ${height}`} preserveAspectRatio="none" className="h-9 w-full" aria-hidden>
			<defs>
				<linearGradient id={gradientId} x1="0" y1="0" x2="0" y2="1">
					<stop offset="0%" stopColor={stroke} stopOpacity="0.22" />
					<stop offset="100%" stopColor={stroke} stopOpacity="0" />
				</linearGradient>
			</defs>
			<polygon points={area} fill={`url(#${gradientId})`} />
			<polyline
				points={line}
				fill="none"
				stroke={stroke}
				strokeWidth="1.5"
				strokeLinecap="round"
				strokeLinejoin="round"
				vectorEffect="non-scaling-stroke"
			/>
		</svg>
	);
}

export function StatTile({
	label,
	value,
	hint,
	points,
	tone = "accent",
}: {
	label: string;
	value: string;
	hint?: string;
	points?: number[];
	tone?: "accent" | "muted";
}) {
	return (
		<div className={cn("bg-card rounded-sm border p-4 shadow-sm")}>
			<p className="text-muted-foreground text-xs">{label}</p>
			<p className="mt-2 text-2xl font-semibold tracking-tight tabular-nums">{value}</p>
			{hint ? <p className="text-muted-foreground mt-1 text-[11px]">{hint}</p> : null}
			{points ? (
				<div className="mt-2 opacity-80">
					<Sparkline points={points} tone={tone} />
				</div>
			) : null}
		</div>
	);
}
