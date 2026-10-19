import { createFileRoute } from "@tanstack/react-router";
import { RefreshCw, Search } from "lucide-react";
import { useMemo, useState } from "react";

import { AppShell } from "@/components/appShell";
import { StatusPill } from "@/components/statusPill";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { api } from "@/lib/api";
import { formatDateTime, formatNumber } from "@/lib/format";
import { useApi } from "@/lib/useApi";

export const Route = createFileRoute("/audit")({
	component: AuditPage,
});

function AuditPage() {
	const [query, setQuery] = useState("");
	const [action, setAction] = useState("");
	const [version, setVersion] = useState(0);
	const state = useApi(
		() => api.audit({ q: query, action, limit: 300 }),
		[query, action, version],
	);
	const items = state.data?.items ?? [];
	const filtered = useMemo(
		() => items.filter((entry) => entry.action.startsWith(action)),
		[items, action],
	);

	return (
		<AppShell>
			<div className="mb-5 flex flex-wrap items-end justify-between gap-3">
				<div>
					<h1 className="text-xl font-semibold tracking-tight">审计日志</h1>
					<p className="text-muted-foreground mt-1 text-sm">
						记录登录、仓库变更、上传/删除与安全配置操作。最多保留最近 5000 条。
					</p>
				</div>
				<Button variant="outline" size="sm" className="h-8" onClick={() => setVersion((value) => value + 1)}>
					<RefreshCw className="size-3.5" />
					刷新
				</Button>
			</div>

			<Card className="gap-0 py-0">
				<div className="flex flex-wrap items-center gap-2 border-b px-3 py-2.5">
					<div className="relative">
						<Search className="text-muted-foreground pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2" />
						<Input
							value={query}
							onChange={(event) => setQuery(event.target.value)}
							placeholder="搜索用户、仓库、路径或消息…"
							className="h-8 w-72 pl-8 text-xs"
						/>
					</div>
					<Input
						value={action}
						onChange={(event) => setAction(event.target.value)}
						placeholder="动作前缀，如 security. / artifact."
						className="h-8 w-56 font-mono text-xs"
					/>
					<span className="text-muted-foreground ml-auto text-[11px]">
						{formatNumber(filtered.length)} / {formatNumber(state.data?.total ?? 0)} 条
					</span>
				</div>
				<div className="overflow-x-auto">
					<table className="w-full text-sm">
						<thead>
							<tr className="bg-muted/50 text-muted-foreground">
								<Th>时间</Th>
								<Th>用户</Th>
								<Th>动作</Th>
								<Th>仓库</Th>
								<Th>路径</Th>
								<Th className="text-right">状态</Th>
							</tr>
						</thead>
						<tbody>
							{state.loading ? (
								Array.from({ length: 6 }).map((_, index) => (
									<tr key={index} className="border-b last:border-0">
										<td className="px-4 py-3" colSpan={6}><Skeleton className="h-5 w-full" /></td>
									</tr>
								))
							) : filtered.length === 0 ? (
								<tr>
									<td className="text-muted-foreground px-4 py-12 text-center text-sm" colSpan={6}>
										暂无审计记录
									</td>
								</tr>
							) : (
								filtered.map((entry) => (
									<tr key={entry.id} className="hover:bg-muted/50 border-b last:border-0">
										<td className="text-muted-foreground px-4 py-2.5 font-mono text-[11px] whitespace-nowrap">
											{formatDateTime(entry.at)}
										</td>
										<td className="px-3 py-2.5">
											<span className="font-mono text-xs">{entry.user || "anonymous"}</span>
											{entry.ip ? <span className="text-muted-foreground ml-1 text-[10px]">{entry.ip}</span> : null}
										</td>
										<td className="px-3 py-2.5">
											<code className="bg-muted rounded border px-1.5 py-0.5 font-mono text-[10px]">{entry.action}</code>
										</td>
										<td className="px-3 py-2.5 font-mono text-[11px]">{entry.repository || "—"}</td>
										<td className="px-3 py-2.5">
											<div className="max-w-[280px] truncate font-mono text-[11px]" title={entry.path || entry.message || ""}>{entry.path || entry.message || "—"}</div>
										</td>
										<td className="px-3 py-2.5 text-right">
											{entry.status ? (
												<StatusPill tone={entry.status < 400 ? "info" : "danger"}>
													{entry.status}
												</StatusPill>
											) : (
												<span className="text-muted-foreground text-[11px]">—</span>
											)}
										</td>
									</tr>
								))
							)}
						</tbody>
					</table>
				</div>
			</Card>
		</AppShell>
	);
}

function Th({ children, className }: { children: React.ReactNode; className?: string }) {
	return <th className={`px-3 py-2 text-left text-[11px] font-medium ${className ?? ""}`}>{children}</th>;
}
