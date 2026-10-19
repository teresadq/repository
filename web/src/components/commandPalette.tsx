import { useNavigate } from "@tanstack/react-router";
import { CornerDownLeft, Search } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";

import { ALL_PAGES, EXTRA_PAGES } from "@/lib/navigation";
import { cn } from "@/lib/utils";

export function CommandPalette({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
	const navigate = useNavigate();
	const [query, setQuery] = useState("");
	const [active, setActive] = useState(0);
	const inputRef = useRef<HTMLInputElement>(null);

	const items = useMemo(() => {
		const all = [...ALL_PAGES, ...EXTRA_PAGES];
		const normalized = query.trim().toLowerCase();
		if (!normalized) return all;
		return all.filter((item) =>
			`${item.label} ${item.keywords ?? ""} ${item.to}`.toLowerCase().includes(normalized),
		);
	}, [query]);

	useEffect(() => {
		if (!open) return;
		setQuery("");
		setActive(0);
		requestAnimationFrame(() => inputRef.current?.focus());
	}, [open]);

	useEffect(() => {
		setActive(0);
	}, [query]);

	useEffect(() => {
		function onKeyDown(event: KeyboardEvent) {
			if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
				event.preventDefault();
				onOpenChange(!open);
			}
			if (event.key === "Escape" && open) {
				onOpenChange(false);
			}
		}
		window.addEventListener("keydown", onKeyDown);
		return () => window.removeEventListener("keydown", onKeyDown);
	}, [open, onOpenChange]);

	if (!open) return null;

	function choose(item: (typeof items)[number]) {
		onOpenChange(false);
		void navigate({ to: item.to });
	}

	function onInputKeyDown(event: React.KeyboardEvent<HTMLInputElement>) {
		if (event.key === "ArrowDown") {
			event.preventDefault();
			setActive((index) => Math.min(index + 1, items.length - 1));
		} else if (event.key === "ArrowUp") {
			event.preventDefault();
			setActive((index) => Math.max(index - 1, 0));
		} else if (event.key === "Enter" && items[active]) {
			event.preventDefault();
			choose(items[active]);
		}
	}

	return (
		<div
			className="fixed inset-0 z-50 flex items-start justify-center bg-black/30 p-4 pt-[12vh] backdrop-blur-[2px]"
			onMouseDown={() => onOpenChange(false)}
		>
			<div
				className="bg-popover w-full max-w-xl overflow-hidden rounded-xl border shadow-2xl"
				onMouseDown={(event) => event.stopPropagation()}
			>
				<div className="flex items-center gap-2.5 border-b px-3.5">
					<Search className="text-muted-foreground size-4 shrink-0" />
					<input
						ref={inputRef}
						value={query}
						onChange={(event) => setQuery(event.target.value)}
						onKeyDown={onInputKeyDown}
						placeholder="搜索页面、设置或操作…"
						className="placeholder:text-muted-foreground/70 h-11 min-w-0 flex-1 bg-transparent text-sm outline-none"
					/>
					<kbd className="text-muted-foreground/70 rounded border px-1.5 py-0.5 font-mono text-[10px]">ESC</kbd>
				</div>

				<div className="max-h-[340px] overflow-y-auto p-1.5">
					{items.length === 0 ? (
						<p className="text-muted-foreground px-3 py-10 text-center text-sm">没有匹配的页面</p>
					) : (
						items.map((item, index) => (
							<button
								key={item.to}
								type="button"
								onMouseEnter={() => setActive(index)}
								onClick={() => choose(item)}
								className={cn(
									"flex w-full cursor-pointer items-center gap-2.5 rounded-md px-2.5 py-2 text-left text-[13px]",
									index === active ? "bg-accent text-accent-foreground" : "text-foreground",
								)}
							>
								<item.icon className="text-muted-foreground size-4 shrink-0" />
								<span>{item.label}</span>
								<span className="text-muted-foreground ml-auto font-mono text-[10px]">{item.to}</span>
								{index === active && <CornerDownLeft className="text-muted-foreground size-3.5" />}
							</button>
						))
					)}
				</div>

				<div className="text-muted-foreground flex gap-4 border-t px-3.5 py-2 text-[11px]">
					<span>↑↓ 选择</span>
					<span>↵ 打开</span>
					<span>esc 关闭</span>
					<span className="ml-auto">⌘K 随时呼出</span>
				</div>
			</div>
		</div>
	);
}
