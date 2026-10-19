import { createFileRoute } from "@tanstack/react-router";
import { Activity, Database, Server, ShieldCheck } from "lucide-react";

import { AppShell } from "@/components/appShell";
import { StatTile } from "@/components/statTile";
import { StatusPill } from "@/components/statusPill";
import { Card } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { api } from "@/lib/api";
import { formatBytes, formatDateTime, formatNumber, formatUptime, repositoryTypeLabel } from "@/lib/format";
import { useApi } from "@/lib/useApi";

export const Route = createFileRoute("/status")({
	component: StatusPage,
});

function StatusPage() {
	const { data, loading, error } = useApi(() => api.status(), []);

	return (
		<AppShell>
			<div className="mb-5">
				<h1 className="text-xl font-semibold tracking-tight">系统状态</h1>
				<p className="text-muted-foreground mt-1 text-sm">
					服务健康、制品协议入口、权限与上游代理熔断状态。
				</p>
			</div>

			{error ? (
				<Card className="border-destructive/30 bg-destructive/5 py-5">
					<p className="text-destructive text-sm">{error}</p>
				</Card>
			) : null}

			<div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-4">
				<StatTile label="服务状态" value={loading ? "—" : "运行中"} hint={data ? `版本 ${data.version}` : undefined} />
				<StatTile label="运行时长" value={loading ? "—" : formatUptime(data?.uptimeSeconds ?? 0)} hint="自进程启动以来" />
				<StatTile
					label="仓库"
					value={loading ? "—" : formatNumber(data?.overview.repositories ?? 0)}
					hint={`${data?.overview.online ?? 0} 在线 · ${data?.overview.groups ?? 0} 分组`}
				/>
				<StatTile label="存储占用" value={loading ? "—" : formatBytes(data?.overview.bytes ?? 0)} hint="所有仓库资产" />
			</div>

			<div className="mt-3 grid grid-cols-1 gap-3 xl:grid-cols-3">
				<HealthCard
					icon={<Server className="size-4" />}
					title="HTTP API"
					description="控制台与协议入口共用同一个 HTTP 服务。"
					status="健康"
					detail={data?.version ? `kkrepo-go ${data.version}` : undefined}
				/>
				<HealthCard
					icon={<Activity className="size-4" />}
					title="Artifact protocols"
					description="支持 Maven、npm、PyPI、Go、Docker、Helm 等 hosted / proxy / group。"
					status={data ? `${data.overview.proxy + data.overview.groups} 个协议仓库` : undefined}
					detail="/repository/{repo}/..."
				/>
				<HealthCard
					icon={<Database className="size-4" />}
					title="元数据与 Blob"
					description="JSON 元数据 + 内容寻址 blob 文件。"
					status={data ? formatBytes(data.overview.bytes) : undefined}
					detail={data?.dataDir}
				/>
			</div>

			<Card className="mt-3 gap-0 py-0">
				<div className="flex items-center justify-between border-b px-4 py-3">
					<div>
						<h2 className="text-sm font-semibold">仓库健康</h2>
						<p className="text-muted-foreground mt-0.5 text-[11px]">在线状态与代理上游熔断信息</p>
					</div>
					<StatusPill tone="info" dot>
						{data ? `${data.repositories.length} 个仓库` : "—"}
					</StatusPill>
				</div>
				<div className="overflow-x-auto">
					<table className="w-full text-sm">
						<thead>
							<tr className="bg-muted/50 text-muted-foreground">
								<th className="px-4 py-2 text-left text-[11px] font-medium">仓库</th>
								<th className="px-3 py-2 text-left text-[11px] font-medium">类型</th>
								<th className="px-3 py-2 text-left text-[11px] font-medium">状态</th>
								<th className="px-3 py-2 text-left text-[11px] font-medium">上游健康</th>
								<th className="px-3 py-2 text-right text-[11px] font-medium">最近成功</th>
							</tr>
						</thead>
						<tbody>
							{loading ? (
								Array.from({ length: 3 }).map((_, index) => (
									<tr key={index} className="border-b last:border-0">
										<td className="px-4 py-3" colSpan={5}>
											<Skeleton className="h-5 w-full" />
										</td>
									</tr>
								))
							) : (
								data?.repositories.map((repo) => (
									<tr key={repo.name} className="hover:bg-muted/50 border-b last:border-0">
										<td className="px-4 py-2.5 font-mono text-xs">{repo.name}</td>
										<td className="text-muted-foreground px-3 py-2.5 text-xs">{repositoryTypeLabel(repo.type)}</td>
										<td className="px-3 py-2.5">
											<StatusPill tone={repo.online ? "info" : "neutral"} dot>
												{repo.online ? "在线" : "离线"}
											</StatusPill>
										</td>
										<td className="px-3 py-2.5 text-xs">
											{repo.proxyState ? (
												repo.proxyState.blocked ? (
													<span className="text-destructive">熔断中 · {repo.proxyState.failCount} 次失败</span>
												) : (
													<span className="text-muted-foreground">
														{repo.proxyState.failCount === 0 ? "正常" : `${repo.proxyState.failCount} 次连续失败`}
													</span>
												)
											) : (
												<span className="text-muted-foreground">—</span>
											)}
										</td>
										<td className="text-muted-foreground px-3 py-2.5 text-right font-mono text-[11px]">
											{repo.proxyState ? formatDateTime(repo.proxyState.lastSuccessAt) : "—"}
										</td>
									</tr>
								))
							)}
						</tbody>
					</table>
				</div>
			</Card>

			<div className="mt-3 grid grid-cols-1 gap-3 xl:grid-cols-2">
				<Card className="gap-0 py-0">
					<div className="border-b px-4 py-3">
						<h2 className="text-sm font-semibold">健康检查</h2>
						<p className="text-muted-foreground mt-0.5 text-[11px]">供负载均衡与监控探活</p>
					</div>
					<div className="space-y-2 p-4 font-mono text-[11px]">
						<p className="text-muted-foreground">
							GET <span className="text-foreground">/api/health</span>
						</p>
						<pre className="bg-muted/60 overflow-x-auto rounded-sm border p-2.5">
{`{
  "status": "ok",
  "version": "${data?.version ?? "0.1.0"}",
  "goVersion": "${data?.goVersion ?? "go1.x"}",
  "uptimeSeconds": ${data?.uptimeSeconds ?? 0}
}`}
						</pre>
					</div>
				</Card>
				<Card className="gap-0 py-0">
					<div className="border-b px-4 py-3">
						<h2 className="text-sm font-semibold">运行时</h2>
						<p className="text-muted-foreground mt-0.5 text-[11px]">构建与数据目录信息</p>
					</div>
					<dl className="divide-y text-sm">
						<RuntimeRow label="控制台版本" value={data?.version ?? "—"} />
						<RuntimeRow label="Go 运行时" value={data?.goVersion ?? "—"} />
						<RuntimeRow label="数据目录" value={data?.dataDir ?? "—"} />
						<RuntimeRow label="启动时间" value={data ? formatDateTime(new Date(Date.now() - data.uptimeSeconds * 1000).toISOString()) : "—"} />
					</dl>
				</Card>
			</div>
		</AppShell>
	);
}

function HealthCard({
	icon,
	title,
	description,
	status,
	detail,
}: {
	icon: React.ReactNode;
	title: string;
	description: string;
	status?: string;
	detail?: string;
}) {
	return (
		<Card className="gap-2 p-4">
			<div className="flex items-center gap-2">
				<span className="bg-muted text-muted-foreground inline-flex size-7 items-center justify-center rounded-sm">
					{icon}
				</span>
				<h2 className="text-sm font-semibold">{title}</h2>
				<ShieldCheck className="text-chart-success ml-auto size-4" />
			</div>
			<p className="text-muted-foreground text-[11px]">{description}</p>
			<div className="flex items-center justify-between border-t pt-2">
				<StatusPill tone="info" dot>
					{status ?? "—"}
				</StatusPill>
				{detail ? <span className="text-muted-foreground max-w-[60%] truncate font-mono text-[10px]">{detail}</span> : null}
			</div>
		</Card>
	);
}

function RuntimeRow({ label, value }: { label: string; value: string }) {
	return (
		<div className="flex items-center justify-between gap-3 px-4 py-2.5">
			<dt className="text-muted-foreground text-xs">{label}</dt>
			<dd className="max-w-[65%] truncate text-right font-mono text-[11px]">{value}</dd>
		</div>
	);
}
