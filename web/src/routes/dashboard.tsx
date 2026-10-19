import { createFileRoute, Link } from "@tanstack/react-router";
import { ArrowUpRight, Boxes, RefreshCw } from "lucide-react";
import {
	Area,
	AreaChart,
	CartesianGrid,
	Cell,
	Pie,
	PieChart,
	ResponsiveContainer,
	Tooltip,
	XAxis,
	YAxis,
} from "recharts";

import { AppShell } from "@/components/appShell";
import { StatTile } from "@/components/statTile";
import { StatusPill } from "@/components/statusPill";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { api } from "@/lib/api";
import { useApi } from "@/lib/useApi";
import { formatBytes, formatDate, formatNumber, kindLabel, relativeTime, sourceLabel } from "@/lib/format";

export const Route = createFileRoute("/dashboard")({
	component: DashboardPage,
});

const TYPE_COLORS = ["var(--chart-cat-1)", "var(--chart-cat-2)", "var(--chart-cat-4)"];

function greeting() {
	const hour = new Date().getHours();
	if (hour < 6) return "夜深了";
	if (hour < 12) return "早上好";
	if (hour < 18) return "下午好";
	return "晚上好";
}

function DashboardPage() {
	const { data, loading, error, reload } = useApi(() => api.overview(), []);

	const overview = data?.overview;
	const daily = overview?.daily ?? [];
	const spark = daily.map((point) => point.assets);
	const typeData = overview
		? [
				{ name: "本地", value: overview.hosted, color: TYPE_COLORS[0] },
				{ name: "代理", value: overview.proxy, color: TYPE_COLORS[1] },
				{ name: "分组", value: overview.groups, color: TYPE_COLORS[2] },
			]
		: [];

	return (
		<AppShell>
			<div className="mb-5 flex flex-wrap items-end justify-between gap-3">
				<div>
					<h1 className="text-xl font-semibold tracking-tight">概览</h1>
					<p className="text-muted-foreground mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-sm">
						<span>
							{greeting()}，这是 kkrepo-go 最近的仓库与缓存运行情况。
						</span>
						<span className="inline-flex items-center gap-1.5 text-xs">
							<span className="bg-chart-success size-1.5 rounded-full" />
							<span className="text-muted-foreground">{overview?.online ?? 0} 个仓库在线</span>
						</span>
					</p>
				</div>
				<Button variant="outline" size="sm" className="h-8" onClick={reload}>
					<RefreshCw className="size-3.5" />
					刷新数据
				</Button>
			</div>

			{error ? (
				<Card className="mb-3 border-destructive/30 bg-destructive/5 py-4">
					<p className="text-destructive text-sm">{error}</p>
				</Card>
			) : null}

			<div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-4">
				<StatTile
					label="仓库总数"
					value={loading ? "—" : formatNumber(overview?.repositories ?? 0)}
					hint={`${overview?.online ?? 0} 在线 · ${overview?.hosted ?? 0} 本地 / ${overview?.proxy ?? 0} 代理 / ${overview?.groups ?? 0} 分组`}
					points={spark}
				/>
				<StatTile
					label="模块版本"
					value={loading ? "—" : formatNumber(overview?.components ?? 0)}
					hint="所有仓库的组件版本数"
				/>
				<StatTile
					label="制品资产"
					value={loading ? "—" : formatNumber(overview?.assets ?? 0)}
					hint="缓存的 info / mod / zip 与元数据"
					points={daily.map((point) => point.bytes / 1024)}
					tone="muted"
				/>
				<StatTile
					label="存储占用"
					value={loading ? "—" : formatBytes(overview?.bytes ?? 0)}
					hint="内容寻址 blob 去重后的大小"
				/>
			</div>

			<div className="mt-3 grid grid-cols-1 gap-3 xl:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]">
				<Card className="gap-0 py-0">
					<div className="flex items-start justify-between gap-2 border-b px-4 py-3">
						<div>
							<h2 className="text-sm font-semibold">近 14 天资产活动</h2>
							<p className="text-muted-foreground mt-0.5 text-[11px]">本地上传与代理缓存产生的资产数量</p>
						</div>
						<StatusPill tone="info" dot>
							{daily.reduce((total, point) => total + point.assets, 0)} 个新增
						</StatusPill>
					</div>
					<div className="h-[280px] px-2 py-3">
						{loading ? (
							<Skeleton className="h-full w-full" />
						) : (
							<ResponsiveContainer width="100%" height="100%">
								<AreaChart data={daily} margin={{ top: 12, right: 16, left: 0, bottom: 0 }}>
									<defs>
										<linearGradient id="activity" x1="0" y1="0" x2="0" y2="1">
											<stop offset="0%" stopColor="var(--chart-cat-1)" stopOpacity={0.28} />
											<stop offset="100%" stopColor="var(--chart-cat-1)" stopOpacity={0} />
										</linearGradient>
									</defs>
									<CartesianGrid stroke="var(--border)" strokeDasharray="3 3" vertical={false} />
									<XAxis
										dataKey="date"
										tickFormatter={(value: string) => value.slice(5)}
										tick={{ fill: "var(--muted-foreground)", fontSize: 11 }}
										axisLine={false}
										tickLine={false}
									/>
									<YAxis
										allowDecimals={false}
										tick={{ fill: "var(--muted-foreground)", fontSize: 11 }}
										axisLine={false}
										tickLine={false}
										width={32}
									/>
									<Tooltip
										contentStyle={{
											background: "var(--popover)",
											border: "1px solid var(--border)",
											borderRadius: 6,
											fontSize: 12,
											color: "var(--popover-foreground)",
										}}
										formatter={(value) => [`${String(value ?? 0)} 个资产`, "新增"]}
									/>
									<Area
										type="monotone"
										dataKey="assets"
										stroke="var(--chart-cat-1)"
										strokeWidth={2}
										fill="url(#activity)"
										isAnimationActive={false}
									/>
								</AreaChart>
							</ResponsiveContainer>
						)}
					</div>
				</Card>

				<Card className="gap-0 py-0">
					<div className="border-b px-4 py-3">
						<h2 className="text-sm font-semibold">仓库构成</h2>
						<p className="text-muted-foreground mt-0.5 text-[11px]">按仓库类型统计</p>
					</div>
					<div className="flex h-[280px] flex-col items-center justify-center gap-2 px-4">
						{loading ? (
							<Skeleton className="h-40 w-40 rounded-full" />
						) : (
							<>
								<div className="h-[170px] w-full">
									<ResponsiveContainer width="100%" height="100%">
										<PieChart>
											<Pie
												data={typeData}
												dataKey="value"
												nameKey="name"
												innerRadius={52}
												outerRadius={72}
												paddingAngle={2}
												isAnimationActive={false}
											>
												{typeData.map((entry) => (
													<Cell key={entry.name} fill={entry.color} />
												))}
											</Pie>
											<Tooltip
												contentStyle={{
													background: "var(--popover)",
													border: "1px solid var(--border)",
													borderRadius: 6,
													fontSize: 12,
													color: "var(--popover-foreground)",
												}}
											/>
										</PieChart>
									</ResponsiveContainer>
								</div>
								<div className="flex flex-wrap items-center justify-center gap-x-4 gap-y-1">
									{typeData.map((entry) => (
										<span key={entry.name} className="text-muted-foreground inline-flex items-center gap-1.5 text-[11px]">
											<span className="size-1.5 rounded-full" style={{ background: entry.color }} />
											{entry.name}
											<span className="text-foreground font-mono tabular-nums">{entry.value}</span>
										</span>
									))}
								</div>
							</>
						)}
					</div>
				</Card>
			</div>

			<div className="mt-3 grid grid-cols-1 gap-3 xl:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]">
				<Card className="gap-0 py-0">
					<div className="flex items-center justify-between border-b px-4 py-3">
						<div>
							<h2 className="text-sm font-semibold">最近活动</h2>
							<p className="text-muted-foreground mt-0.5 text-[11px]">最新的上传、缓存与元数据刷新</p>
						</div>
						<Button variant="ghost" size="sm" className="h-7 text-xs" asChild>
							<Link to="/repositories">
								全部仓库
								<ArrowUpRight className="size-3.5" />
							</Link>
						</Button>
					</div>
					<div className="overflow-x-auto">
						<table className="w-full text-sm">
							<thead>
								<tr className="bg-muted/50 text-muted-foreground">
									<th className="px-4 py-2 text-left text-[11px] font-medium">模块</th>
									<th className="px-3 py-2 text-left text-[11px] font-medium">版本</th>
									<th className="px-3 py-2 text-left text-[11px] font-medium">仓库</th>
									<th className="px-3 py-2 text-left text-[11px] font-medium">类型</th>
									<th className="px-3 py-2 text-left text-[11px] font-medium">资产</th>
									<th className="px-3 py-2 text-right text-[11px] font-medium">大小</th>
									<th className="px-4 py-2 text-right text-[11px] font-medium">时间</th>
								</tr>
							</thead>
							<tbody>
								{loading ? (
									Array.from({ length: 5 }).map((_, index) => (
										<tr key={index} className="border-b last:border-0">
											<td className="px-4 py-2.5" colSpan={7}>
												<Skeleton className="h-4 w-full" />
											</td>
										</tr>
									))
								) : (data?.recent ?? []).length === 0 ? (
									<tr>
										<td className="text-muted-foreground px-4 py-10 text-center text-sm" colSpan={7}>
											还没有任何活动。通过 <span className="font-mono">GOPROXY</span> 拉取一个模块，或上传一个 zip 试试。
										</td>
									</tr>
								) : (
									data?.recent.map((item) => (
										<tr key={`${item.repository}-${item.path}`} className="hover:bg-muted/50 border-b last:border-0">
											<td className="px-4 py-2.5">
												<div className="max-w-[200px] truncate font-mono text-xs" title={item.module}>{item.module}</div>
											</td>
											<td className="px-3 py-2.5">
												<div className="max-w-[110px] truncate font-mono text-xs" title={item.version || ""}>{item.version || "—"}</div>
											</td>
											<td className="px-3 py-2.5">
												<Link
													to="/repositories/$name"
													params={{ name: item.repository }}
													className="hover:text-foreground text-muted-foreground font-mono text-xs underline-offset-4 hover:underline"
												>
													<span className="block max-w-[110px] truncate">{item.repository}</span>
												</Link>
											</td>
											<td className="px-3 py-2.5">
												<StatusPill tone={item.source === "proxy" ? "info" : "neutral"}>
													{sourceLabel(item.source)}
												</StatusPill>
											</td>
											<td className="px-3 py-2.5">
												<StatusPill tone={item.kind === "PACKAGE" ? "info" : "neutral"}>
													{kindLabel(item.kind)}
												</StatusPill>
											</td>
											<td className="px-3 py-2.5 text-right font-mono text-xs whitespace-nowrap tabular-nums">{formatBytes(item.size)}</td>
											<td className="text-muted-foreground px-4 py-2.5 text-right font-mono text-[11px] whitespace-nowrap">
												{relativeTime(item.updatedAt)}
											</td>
										</tr>
									))
								)}
							</tbody>
						</table>
					</div>
				</Card>

				<Card className="gap-0 py-0">
					<div className="border-b px-4 py-3">
						<h2 className="text-sm font-semibold">模块排行</h2>
						<p className="text-muted-foreground mt-0.5 text-[11px]">按版本数量排序</p>
					</div>
					<div className="space-y-3 p-4">
						{loading ? (
							Array.from({ length: 4 }).map((_, index) => <Skeleton key={index} className="h-10 w-full" />)
						) : (overview?.topModules ?? []).length === 0 ? (
							<div className="text-muted-foreground flex flex-col items-center gap-2 py-8 text-center text-xs">
								<Boxes className="size-6 opacity-50" />
								暂无模块数据
							</div>
						) : (
							overview?.topModules.map((module, index) => {
								const max = overview.topModules[0]?.versions || 1;
								return (
									<div key={module.module} className="space-y-1.5">
										<div className="flex items-center gap-2">
											<span className="text-muted-foreground w-4 font-mono text-[10px]">{index + 1}</span>
											<span className="flex-1 truncate font-mono text-xs">{module.module}</span>
											<span className="text-muted-foreground font-mono text-[11px] tabular-nums">
												{module.versions} 版本
											</span>
										</div>
										<div className="bg-muted ml-6 h-1 overflow-hidden rounded-full">
											<div
												className="bg-chart-cat-1 h-full rounded-full"
												style={{ width: `${Math.max(6, (module.versions / max) * 100)}%` }}
											/>
										</div>
									</div>
								);
							})
						)}
					</div>
				</Card>
			</div>

			<p className="text-muted-foreground/70 mt-4 text-center text-[11px]">
				kkrepo-go · 设计参考 Bifrost UI 设计系统 · {formatDate(new Date().toISOString())} 数据快照
			</p>
		</AppShell>
	);
}
