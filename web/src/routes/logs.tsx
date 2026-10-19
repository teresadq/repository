import { createFileRoute } from "@tanstack/react-router";
import { ExternalLink, RefreshCw } from "lucide-react";
import { useEffect, useMemo, useState } from "react";

import { AppShell } from "@/components/appShell";
import { StatTile } from "@/components/statTile";
import { StatusPill } from "@/components/statusPill";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { api } from "@/lib/api";
import { formatBytes, formatDateTime, formatNumber } from "@/lib/format";
import { useApi } from "@/lib/useApi";

export const Route = createFileRoute("/logs")({
	component: LogsPage,
});

function LogsPage() {
	const [filter, setFilter] = useState("");
	const [live, setLive] = useState(false);
	const [version, setVersion] = useState(0);
	const state = useApi(() => api.logs(400), [version]);

	useEffect(() => {
		if (!live) return;
		const timer = window.setInterval(() => setVersion((value) => value + 1), 5000);
		return () => window.clearInterval(timer);
	}, [live]);

	const items = state.data?.items ?? [];
	const filtered = useMemo(() => {
		const normalized = filter.trim().toLowerCase();
		if (!normalized) return items;
		return items.filter((item) =>
			`${item.method} ${item.path} ${item.repository} ${item.format} ${item.user} ${item.status}`
				.toLowerCase()
				.includes(normalized),
		);
	}, [items, filter]);

	const totalBytes = items.reduce((sum, item) => sum + item.bytes, 0);
	const errorCount = items.filter((item) => item.status >= 400).length;

	return (
		<AppShell>
			<div className="mb-5 flex flex-wrap items-end justify-between gap-3">
				<div>
					<h1 className="text-xl font-semibold tracking-tight">请求日志</h1>
					<p className="text-muted-foreground mt-1 text-sm">
						进程内最近 1000 条请求；Prometheus 指标通过{" "}
						<a href="/metrics" target="_blank" rel="noreferrer" className="text-foreground inline-flex items-center gap-1 underline underline-offset-4">
							/metrics <ExternalLink className="size-3" />
						</a>{" "}
						暴露。
					</p>
				</div>
				<div className="flex items-center gap-3">
					<label className="text-muted-foreground flex items-center gap-2 text-xs">
						实时刷新
						<Switch checked={live} onCheckedChange={setLive} aria-label="实时刷新" />
					</label>
					<Button variant="outline" size="sm" className="h-8" onClick={() => setVersion((value) => value + 1)}>
						<RefreshCw className="size-3.5" />
						刷新
					</Button>
				</div>
			</div>

			<div className="mb-3 grid grid-cols-1 gap-3 sm:grid-cols-3">
				<StatTile label="最近请求" value={formatNumber(items.length)} hint="当前缓冲区中的请求数" />
				<StatTile label="错误请求" value={formatNumber(errorCount)} hint="HTTP 状态 >= 400" />
				<StatTile label="响应字节" value={formatBytes(totalBytes)} hint="最近请求的响应体总量" />
			</div>

			<Card className="gap-0 py-0">
				<div className="flex flex-wrap items-center gap-2 border-b px-3 py-2.5">
					<Input
						value={filter}
						onChange={(event) => setFilter(event.target.value)}
						placeholder="筛选方法、路径、仓库、用户或状态…"
						className="h-8 w-80 text-xs"
					/>
					<span className="text-muted-foreground ml-auto text-[11px]">
						{formatNumber(filtered.length)} / {formatNumber(items.length)} 条
					</span>
				</div>
				<div className="overflow-x-auto">
					<table className="w-full text-sm">
						<thead>
							<tr className="bg-muted/50 text-muted-foreground">
								<Th>时间</Th>
								<Th>方法</Th>
								<Th>路径</Th>
								<Th>仓库</Th>
								<Th className="text-right">状态</Th>
								<Th className="text-right">大小</Th>
								<Th className="text-right">耗时</Th>
								<Th>用户</Th>
							</tr>
						</thead>
						<tbody>
							{state.loading ? (
								Array.from({ length: 8 }).map((_, index) => (
									<tr key={index} className="border-b last:border-0">
										<td className="px-4 py-3" colSpan={8}><Skeleton className="h-5 w-full" /></td>
									</tr>
								))
							) : filtered.length === 0 ? (
								<tr>
									<td className="text-muted-foreground px-4 py-12 text-center text-sm" colSpan={8}>
										暂无请求记录
									</td>
								</tr>
							) : (
								filtered.map((item, index) => (
									<tr key={`${item.at}-${index}`} className="hover:bg-muted/50 border-b last:border-0">
										<td className="text-muted-foreground px-4 py-2.5 font-mono text-[11px] whitespace-nowrap">
											{formatDateTime(item.at)}
										</td>
										<td className="px-3 py-2.5">
											<StatusPill tone={item.method === "GET" || item.method === "HEAD" ? "neutral" : "purple"}>
												{item.method}
											</StatusPill>
										</td>
										<td className="px-3 py-2.5">
											<div className="max-w-[320px] truncate font-mono text-[11px]" title={item.path}>{item.path}</div>
										</td>
										<td className="px-3 py-2.5 font-mono text-[11px]">{item.repository || "—"}</td>
										<td className="px-3 py-2.5 text-right">
											<StatusPill tone={item.status < 400 ? "info" : item.status < 500 ? "warning" : "danger"}>
												{item.status}
											</StatusPill>
										</td>
										<td className="px-3 py-2.5 text-right font-mono text-[11px] tabular-nums">{formatBytes(item.bytes)}</td>
										<td className="text-muted-foreground px-3 py-2.5 text-right font-mono text-[11px]">{item.durationMs} ms</td>
										<td className="px-3 py-2.5 font-mono text-[11px]">{item.user || "—"}</td>
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
