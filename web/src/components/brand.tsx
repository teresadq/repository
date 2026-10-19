import { cn } from "@/lib/utils";

/**
 * 中性品牌标记：深色圆角方块 + 白色几何斜杠，颜色跟随
 * foreground/background 令牌，自动适配明暗主题。
 */
export function LogoMark({ className }: { className?: string }) {
	return (
		<span
			className={cn(
				"bg-foreground text-background inline-flex size-8 shrink-0 items-center justify-center rounded-md",
				className,
			)}
		>
			<svg viewBox="0 0 24 24" fill="none" className="size-4" aria-hidden>
				<path d="M5 6h5l9 12h-5L5 6Z" fill="currentColor" />
				<path d="M16 6h3v6l-3-4V6Z" fill="currentColor" opacity="0.55" />
			</svg>
		</span>
	);
}

export function Brand({ className, markClassName }: { className?: string; markClassName?: string }) {
	return (
		<span className={cn("flex items-center gap-2", className)}>
			<LogoMark className={markClassName} />
			<span className="text-[17px] leading-none font-semibold tracking-tight">
				kkrepo<span className="text-muted-foreground font-mono text-[12px] font-medium">-go</span>
			</span>
		</span>
	);
}
