import { createFileRoute } from "@tanstack/react-router";
import { Play, Plus, RefreshCw, Search, Trash2 } from "lucide-react";
import { useMemo, useState } from "react";
import { toast } from "sonner";

import { AppShell } from "@/components/appShell";
import { StatusPill } from "@/components/statusPill";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { api, type CleanupPolicy, type CleanupRun } from "@/lib/api";
import { formatBytes, formatDateTime, formatNumber } from "@/lib/format";
import { errorMessage } from "@/lib/repository";
import { useApi } from "@/lib/useApi";

export const Route = createFileRoute("/cleanup")({
	component: CleanupPage,
});

function criteria(policy: CleanupPolicy): string {
	const parts: string[] = [];
	if (policy.pattern) parts.push(`路径 ${policy.pattern}`);
	if (policy.publishedOlderThanDays) parts.push(`发布 > ${policy.publishedOlderThanDays} 天`);
	if (policy.lastDownloadedOlderThanDays) parts.push(`未下载 > ${policy.lastDownloadedOlderThanDays} 天`);
	if (policy.retainCount) parts.push(`保留 ${policy.retainCount} 个/组件`);
	return parts.join(" · ") || "—";
}

function CleanupPage() {
	const [version, setVersion] = useState(0);
	const policies = useApi(() => api.cleanupPolicies(), [version]);
	const repositories = useApi(() => api.repositories(), [version]);
	const runs = useApi(() => api.cleanupRuns(), [version]);
	const [createOpen, setCreateOpen] = useState(false);
	const [result, setResult] = useState<CleanupRun | null>(null);
	const [busy, setBusy] = useState("");
	const [query, setQuery] = useState("");
	const reload = () => setVersion((value) => value + 1);

	const filtered = useMemo(() => {
		const normalized = query.trim().toLowerCase();
		const items = policies.data?.items ?? [];
		if (!normalized) return items;
		return items.filter((policy) => `${policy.name} ${policy.repository} ${policy.pattern}`.toLowerCase().includes(normalized));
	}, [policies.data, query]);

	async function preview(policy: CleanupPolicy) {
		setBusy(policy.name + ":preview");
		try {
			setResult(await api.previewCleanup(policy.name));
		} catch (err) {
			toast.error("预览失败", { description: errorMessage(err) });
		} finally {
			setBusy("");
		}
	}

	async function run(policy: CleanupPolicy) {
		if (!window.confirm(`执行清理策略 ${policy.name}？删除操作不可撤销。`)) return;
		setBusy(policy.name + ":run");
		try {
			const run = await api.runCleanup(policy.name);
			setResult(run);
			toast.success("清理完成", { description: `删除 ${run.deleted} 个资产` });
			reload();
		} catch (err) {
			toast.error("清理失败", { description: errorMessage(err) });
		} finally {
			setBusy("");
		}
	}

	async function remove(policy: CleanupPolicy) {
		try {
			await api.deleteCleanupPolicy(policy.name);
			toast.success("策略已删除", { description: policy.name });
			reload();
		} catch (err) {
			toast.error("删除失败", { description: errorMessage(err) });
		}
	}

	return (
		<AppShell>
			<div className="mb-5 flex flex-wrap items-end justify-between gap-3">
				<div>
					<h1 className="text-xl font-semibold tracking-tight">清理策略</h1>
					<p className="text-muted-foreground mt-1 text-sm">
						按路径、发布时间、最近下载时间和保留数量清理 hosted 资产；先预览再执行。
					</p>
				</div>
				<div className="flex gap-2">
					<Button variant="outline" size="sm" className="h-8" onClick={reload}>
						<RefreshCw className="size-3.5" />
						刷新
					</Button>
					<Button size="sm" className="h-8" onClick={() => setCreateOpen(true)}>
						<Plus className="size-3.5" />
						新建策略
					</Button>
				</div>
			</div>

			<Card className="gap-0 py-0">
				<div className="flex flex-wrap items-center gap-2 border-b px-3 py-2.5">
					<div className="relative">
						<Search className="text-muted-foreground pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2" />
						<Input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索策略或仓库…" className="h-8 w-64 pl-8 text-xs" />
					</div>
					<span className="text-muted-foreground ml-auto text-[11px]">{filtered.length} 个策略</span>
				</div>
				<div className="overflow-x-auto">
					<table className="w-full text-sm">
						<thead>
							<tr className="bg-muted/50 text-muted-foreground">
								<Th>策略</Th>
								<Th>仓库</Th>
								<Th>条件</Th>
								<Th>状态</Th>
								<Th className="text-right">操作</Th>
							</tr>
						</thead>
						<tbody>
							{policies.loading ? (
								Array.from({ length: 3 }).map((_, index) => (
									<tr key={index} className="border-b last:border-0"><td className="px-4 py-3" colSpan={5}><Skeleton className="h-5 w-full" /></td></tr>
								))
							) : filtered.length === 0 ? (
								<tr><td className="text-muted-foreground px-4 py-10 text-center text-sm" colSpan={5}>暂无清理策略</td></tr>
							) : (
								filtered.map((policy) => (
									<tr key={policy.name} className="hover:bg-muted/50 border-b last:border-0">
										<td className="px-4 py-2.5 font-mono text-xs font-medium">{policy.name}</td>
										<td className="px-3 py-2.5 font-mono text-xs">{policy.repository}</td>
										<td className="text-muted-foreground px-3 py-2.5 text-[11px]">{criteria(policy)}</td>
										<td className="px-3 py-2.5"><StatusPill tone={policy.enabled ? "info" : "neutral"} dot>{policy.enabled ? "启用" : "停用"}</StatusPill></td>
										<td className="px-3 py-2.5 text-right">
											<Button variant="ghost" size="sm" className="h-7 text-xs" isLoading={busy === policy.name + ":preview"} onClick={() => void preview(policy)}>
												预览
											</Button>
											<Button variant="ghost" size="sm" className="h-7 text-xs" isLoading={busy === policy.name + ":run"} onClick={() => void run(policy)}>
												<Play className="size-3" />
												执行
											</Button>
											<Button variant="ghost" size="icon" className="text-muted-foreground hover:text-destructive size-7" aria-label={`删除 ${policy.name}`} onClick={() => void remove(policy)}>
												<Trash2 className="size-3.5" />
											</Button>
										</td>
									</tr>
								))
							)}
						</tbody>
					</table>
				</div>
			</Card>

			<Card className="mt-3 gap-0 py-0">
				<div className="border-b px-4 py-3">
					<h2 className="text-sm font-semibold">最近运行</h2>
					<p className="text-muted-foreground mt-0.5 text-[11px]">保留最近 100 次预览/执行记录</p>
				</div>
				<div className="overflow-x-auto">
					<table className="w-full text-sm">
						<thead>
							<tr className="bg-muted/50 text-muted-foreground">
								<Th>时间</Th><Th>策略</Th><Th>模式</Th>
								<Th className="text-right">扫描</Th><Th className="text-right">匹配</Th>
								<Th className="text-right">删除</Th><Th className="text-right">大小</Th>
							</tr>
						</thead>
						<tbody>
							{(runs.data?.items ?? []).length === 0 ? (
								<tr><td className="text-muted-foreground px-4 py-8 text-center text-sm" colSpan={7}>暂无运行记录</td></tr>
							) : (
								runs.data?.items.map((run) => (
									<tr key={run.id} className="border-b last:border-0">
										<td className="text-muted-foreground px-4 py-2.5 font-mono text-[11px]">{formatDateTime(run.at)}</td>
										<td className="px-3 py-2.5 font-mono text-xs">{run.policy}</td>
										<td className="px-3 py-2.5"><StatusPill tone={run.dryRun ? "neutral" : "warning"}>{run.dryRun ? "预览" : "执行"}</StatusPill></td>
										<td className="px-3 py-2.5 text-right font-mono text-xs">{formatNumber(run.scanned)}</td>
										<td className="px-3 py-2.5 text-right font-mono text-xs">{formatNumber(run.matched)}</td>
										<td className="px-3 py-2.5 text-right font-mono text-xs">{formatNumber(run.deleted)}</td>
										<td className="px-3 py-2.5 text-right font-mono text-xs">{formatBytes(run.bytes)}</td>
									</tr>
								))
							)}
						</tbody>
					</table>
				</div>
			</Card>

			<CreatePolicyDialog
				open={createOpen}
				onOpenChange={setCreateOpen}
				repositories={repositories.data?.items ?? []}
				onCreated={() => { setCreateOpen(false); reload(); }}
			/>

			<Dialog open={!!result} onOpenChange={(open) => !open && setResult(null)}>
				<DialogContent className="sm:max-w-2xl">
					<DialogHeader>
						<DialogTitle>{result?.dryRun ? "清理预览" : "清理结果"}</DialogTitle>
						<DialogDescription>
							扫描 {result?.scanned ?? 0} · 匹配 {result?.matched ?? 0} · 删除 {result?.deleted ?? 0} · {formatBytes(result?.bytes ?? 0)}
						</DialogDescription>
					</DialogHeader>
					<div className="max-h-[420px] overflow-auto rounded-sm border">
						<table className="w-full text-xs">
							<thead><tr className="bg-muted/50 text-muted-foreground"><Th>路径</Th><Th>原因</Th><Th className="text-right">大小</Th></tr></thead>
							<tbody>
								{(result?.items ?? []).length === 0 ? (
									<tr><td className="text-muted-foreground px-3 py-6 text-center" colSpan={3}>没有匹配的资产</td></tr>
								) : result?.items.map((item) => (
									<tr key={item.path} className="border-b last:border-0">
										<td className="px-3 py-2 font-mono">{item.path}</td>
										<td className="text-muted-foreground px-3 py-2">{item.reason}</td>
										<td className="px-3 py-2 text-right font-mono">{formatBytes(item.size)}</td>
									</tr>
								))}
							</tbody>
						</table>
					</div>
					<DialogFooter><Button onClick={() => setResult(null)}>关闭</Button></DialogFooter>
				</DialogContent>
			</Dialog>
		</AppShell>
	);
}

function CreatePolicyDialog({
	open,
	onOpenChange,
	repositories,
	onCreated,
}: {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	repositories: Array<{ name: string; format: string }>;
	onCreated: () => void;
}) {
	const [name, setName] = useState("");
	const [repository, setRepository] = useState("");
	const [pattern, setPattern] = useState("");
	const [publishedDays, setPublishedDays] = useState("");
	const [downloadDays, setDownloadDays] = useState("");
	const [retain, setRetain] = useState("");
	const [saving, setSaving] = useState(false);

	async function submit() {
		setSaving(true);
		try {
			await api.createCleanupPolicy({
				name: name.trim(), repository,
				pattern: pattern.trim(),
				publishedOlderThanDays: Number(publishedDays) || 0,
				lastDownloadedOlderThanDays: Number(downloadDays) || 0,
				retainCount: Number(retain) || 0,
			});
			toast.success("清理策略已创建", { description: name });
			onCreated();
		} catch (err) {
			toast.error("创建失败", { description: errorMessage(err) });
		} finally {
			setSaving(false);
		}
	}

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent className="sm:max-w-lg">
				<DialogHeader>
					<DialogTitle>新建清理策略</DialogTitle>
					<DialogDescription>至少填写一个条件；路径 glob 支持 * 与 **。</DialogDescription>
				</DialogHeader>
				<div className="space-y-3">
					<div><Label>名称</Label><Input value={name} onChange={(event) => setName(event.target.value)} className="mt-1.5 h-9 font-mono" placeholder="cleanup-snapshots" /></div>
					<div>
						<Label>仓库</Label>
						<select value={repository} onChange={(event) => setRepository(event.target.value)} className="bg-background focus-visible:border-primary mt-1.5 h-9 w-full rounded-sm border px-2.5 text-sm outline-none">
							<option value="">选择仓库…</option>
							{repositories.map((repo) => <option key={repo.name} value={repo.name}>{repo.name} · {repo.format}</option>)}
						</select>
					</div>
					<div><Label>路径 glob</Label><Input value={pattern} onChange={(event) => setPattern(event.target.value)} className="mt-1.5 h-9 font-mono text-xs" placeholder="com/acme/**" /></div>
					<div className="grid grid-cols-3 gap-3">
						<div><Label>发布天数</Label><Input value={publishedDays} onChange={(event) => setPublishedDays(event.target.value)} className="mt-1.5 h-9 font-mono" placeholder="90" /></div>
						<div><Label>未下载天数</Label><Input value={downloadDays} onChange={(event) => setDownloadDays(event.target.value)} className="mt-1.5 h-9 font-mono" placeholder="30" /></div>
						<div><Label>保留数量</Label><Input value={retain} onChange={(event) => setRetain(event.target.value)} className="mt-1.5 h-9 font-mono" placeholder="3" /></div>
					</div>
				</div>
				<DialogFooter>
					<Button variant="outline" onClick={() => onOpenChange(false)}>取消</Button>
					<Button isLoading={saving} disabled={!name.trim() || !repository} onClick={() => void submit()}>创建</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}

function Th({ children, className }: { children: React.ReactNode; className?: string }) {
	return <th className={`px-3 py-2 text-left text-[11px] font-medium ${className ?? ""}`}>{children}</th>;
}
