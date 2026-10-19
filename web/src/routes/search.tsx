import { createFileRoute, Link } from "@tanstack/react-router";
import { Download, Search as SearchIcon } from "lucide-react";
import { useState } from "react";

import { AppShell } from "@/components/appShell";
import { StatusPill } from "@/components/statusPill";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { api, type SearchItem } from "@/lib/api";
import { formatBytes, formatDateTime, formatNumber } from "@/lib/format";
import { useApi } from "@/lib/useApi";

export const Route = createFileRoute("/search")({
	component: SearchPage,
});

function SearchPage() {
	const [query, setQuery] = useState("");
	const [type, setType] = useState("assets");
	const [format, setFormat] = useState("");
	const [repository, setRepository] = useState("");
	const repositories = useApi(() => api.repositories(), []);
	const results = useApi(
		() => api.search({ q: query, type, format, repository, limit: 200 }),
		[query, type, format, repository],
	);

	return (
		<AppShell>
			<div className="mb-5">
				<h1 className="text-xl font-semibold tracking-tight">全局搜索</h1>
				<p className="text-muted-foreground mt-1 text-sm">
					按路径、模块、版本或 SHA-256 搜索所有仓库的资产与组件版本。
				</p>
			</div>

			<Card className="mb-3 gap-0 py-0">
				<div className="flex flex-wrap items-end gap-3 p-4">
					<div className="min-w-0 flex-1">
						<label className="text-muted-foreground mb-1 block text-[11px]">关键字 / 校验和</label>
						<div className="relative">
							<SearchIcon className="text-muted-foreground pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2" />
							<Input
								value={query}
								onChange={(event) => setQuery(event.target.value)}
								placeholder="模块名、路径或 SHA-256 前缀…"
								className="h-9 pl-8 text-sm"
							/>
						</div>
					</div>
					<div>
						<label className="text-muted-foreground mb-1 block text-[11px]">类型</label>
						<select value={type} onChange={(event) => setType(event.target.value)} className="bg-background focus-visible:border-primary h-9 rounded-sm border px-2.5 text-sm outline-none">
							<option value="assets">资产</option>
							<option value="components">组件版本</option>
						</select>
					</div>
					<div>
						<label className="text-muted-foreground mb-1 block text-[11px]">格式</label>
						<Input value={format} onChange={(event) => setFormat(event.target.value)} placeholder="全部" className="h-9 w-28 font-mono text-xs" />
					</div>
					<div>
						<label className="text-muted-foreground mb-1 block text-[11px]">仓库</label>
						<select value={repository} onChange={(event) => setRepository(event.target.value)} className="bg-background focus-visible:border-primary h-9 w-44 rounded-sm border px-2.5 text-sm outline-none">
							<option value="">全部仓库</option>
							{(repositories.data?.items ?? []).map((repo) => (
								<option key={repo.name} value={repo.name}>{repo.name}</option>
							))}
						</select>
					</div>
				</div>
			</Card>

			<Card className="gap-0 py-0">
				<div className="border-b px-4 py-3">
					<h2 className="text-sm font-semibold">结果</h2>
					<p className="text-muted-foreground mt-0.5 text-[11px]">
						{results.loading ? "搜索中…" : `${formatNumber(results.data?.total ?? 0)} 条匹配`}
					</p>
				</div>
				<div className="overflow-x-auto">
					<table className="w-full text-sm">
						<thead>
							<tr className="bg-muted/50 text-muted-foreground">
								<Th>仓库</Th>
								<Th>格式</Th>
								<Th>名称</Th>
								<Th>版本</Th>
								<Th>路径</Th>
								<Th className="text-right">大小</Th>
								<Th className="text-right">更新时间</Th>
								<Th className="text-right">下载</Th>
							</tr>
						</thead>
						<tbody>
							{results.loading ? (
								Array.from({ length: 5 }).map((_, index) => (
									<tr key={index} className="border-b last:border-0"><td className="px-4 py-3" colSpan={8}><Skeleton className="h-5 w-full" /></td></tr>
								))
							) : (results.data?.items ?? []).length === 0 ? (
								<tr><td className="text-muted-foreground px-4 py-12 text-center text-sm" colSpan={8}>没有匹配结果</td></tr>
							) : (
								results.data?.items.map((item: SearchItem) => (
									<tr key={`${item.repository}-${item.type}-${item.path ?? item.name}-${item.version ?? ""}`} className="hover:bg-muted/50 border-b last:border-0">
										<td className="px-4 py-2.5">
											<Link to="/repositories/$name" params={{ name: item.repository }} className="font-mono text-xs underline-offset-4 hover:underline">
												{item.repository}
											</Link>
										</td>
										<td className="px-3 py-2.5"><StatusPill tone="neutral">{item.format}</StatusPill></td>
										<td className="max-w-[220px] truncate px-3 py-2.5 font-mono text-xs" title={item.name}>{item.name}</td>
										<td className="px-3 py-2.5 font-mono text-xs">{item.version || "—"}</td>
										<td className="max-w-[300px] truncate px-3 py-2.5 font-mono text-[11px]" title={item.path ?? ""}>{item.path || "—"}</td>
										<td className="px-3 py-2.5 text-right font-mono text-xs tabular-nums">{formatBytes(item.size ?? 0)}</td>
										<td className="text-muted-foreground px-3 py-2.5 text-right font-mono text-[11px] whitespace-nowrap">{formatDateTime(item.updatedAt)}</td>
										<td className="px-3 py-2.5 text-right">
											{item.url ? (
												<Button variant="ghost" size="sm" className="h-7 text-xs" asChild>
													<a href={item.url}>
														<Download className="size-3.5" />
														下载
													</a>
												</Button>
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
