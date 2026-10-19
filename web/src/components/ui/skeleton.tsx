import { cn } from "@/lib/utils";

export function Skeleton({ className }: { className?: string }) {
	return <div className={cn("bg-muted animate-pulse rounded-sm", className)} aria-hidden />;
}
