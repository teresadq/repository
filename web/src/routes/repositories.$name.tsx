import { Link, createFileRoute } from "@tanstack/react-router";
import {
	ArrowLeft,
	ChevronDown,
	ChevronRight,
	Copy,
	Download,
	ExternalLink,
	File,
	Folder,
	FolderOpen,
	RefreshCw,
	Save,
	Trash2,
} from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";

import { AppShell } from "@/components/appShell";
import { StatTile } from "@/components/statTile";
import { StatusPill, type StatusTone } from "@/components/statusPill";
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
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { api, type BrowseEntry, type Repository, type RepositoryInput } from "@/lib/api";
import {
	copyText,
	formatBytes,
	formatDateTime,
	formatNumber,
	kindLabel,
	relativeTime,
	repositoryTypeLabel,
	sourceLabel,
} from "@/lib/format";
import { useApi } from "@/lib/useApi";
import { errorMessage, updatePayload } from "@/lib/repository";

export const Route = createFileRoute("/repositories/$name")({
	component: RepositoryDetailPage,
});

const TYPE_TONES: Record<string, StatusTone> = {
	hosted: "neutral",
	proxy: "info",
	group: "purple",
};

const TABS = [
	{ key: "overview", label: "概览" },
	{ key: "browse", label: "文件树" },
	{ key: "components", label: "组件" },
	{ key: "assets", label: "资产" },
	{ key: "settings", label: "设置" },
] as const;

type TabKey = (typeof TABS)[number]["key"];

function RepositoryDetailPage() {
	const { name } = Route.useParams();
	const [tab, setTab] = useState<TabKey>("overview");
	const [version, setVersion] = useState(0);

	const repoState = useApi(() => api.repository(name), [name, version]);
	const componentState = useApi(() => api.components(name, { limit: 500 }), [name, version]);
	const assetState = useApi(() => api.assets(name, { limit: 500 }), [name, version]);
	const allRepos = useApi(() => api.repositories(), [version]);

	const repo = repoState.data;
	const reloadAll = () => setVersion((value) => value + 1);

	if (repoState.error) {
		return (
			<AppShell>
				<Card className="border-destructive/30 bg-destructive/5 py-6">
					<p className="text-destructive text-sm">{repoState.error}</p>
					<Button variant="outline" size="sm" className="mt-3" asChild>
						<Link to="/repositories">返回仓库列表</Link>
					</Button>
				</Card>
			</AppShell>
		);
	}

	return (
		<AppShell>
			<div className="mb-4">
				<Button variant="ghost" size="sm" className="text-muted-foreground -ml-2 h-7 text-xs" asChild>
					<Link to="/repositories">
						<ArrowLeft className="size-3.5" />
						仓库列表
					</Link>
				</Button>
			</div>

			<div className="mb-5 flex flex-wrap items-start justify-between gap-3">
				<div className="min-w-0">
					<div className="flex flex-wrap items-center gap-2">
						<h1 className="font-mono text-xl font-semibold tracking-tight">{name}</h1>
						{repo ? (
							<>
								<StatusPill tone="neutral">{repo.format}</StatusPill>
								<StatusPill tone={TYPE_TONES[repo.type] ?? "neutral"} dot>
									{repositoryTypeLabel(repo.type)}
								</StatusPill>
								<StatusPill tone={repo.online ? "info" : "neutral"} dot>
									{repo.online ? "在线" : "离线"}
								</StatusPill>
								{repo.proxyState?.blocked ? (
									<StatusPill tone="danger" dot>
										上游熔断
									</StatusPill>
								) : null}
							</>
						) : null}
					</div>
					<p className="text-muted-foreground mt-1.5 text-sm">
						{repo?.description || `${repo?.recipe ?? "go"} 仓库`}
					</p>
				</div>
				<div className="flex flex-wrap items-center gap-2">
					<Button variant="outline" size="sm" className="h-8" onClick={reloadAll}>
						<RefreshCw className="size-3.5" />
						刷新
					</Button>
					<Button
						variant="outline"
						size="sm"
						className="h-8"
						onClick={() => {
							const url = `${window.location.origin}/repository/${name}`;
							void copyText(url).then(() => toast.success("仓库地址已复制", { description: url }));
						}}
					>
						<Copy className="size-3.5" />
						复制地址
					</Button>
					<Button size="sm" className="h-8" asChild>
						<a href={`/repository/${name}`} target="_blank" rel="noreferrer">
							<ExternalLink className="size-3.5" />
							打开协议入口
						</a>
					</Button>
				</div>
			</div>

			<div className="bg-card mb-3 flex items-center gap-1 rounded-sm border p-1 shadow-sm">
				{TABS.map((item) => (
					<button
						key={item.key}
						type="button"
						onClick={() => setTab(item.key)}
						aria-pressed={tab === item.key}
						className={
							tab === item.key
								? "bg-secondary text-foreground cursor-pointer rounded-[3px] px-3 py-1.5 text-xs font-medium shadow-sm"
								: "text-muted-foreground hover:text-foreground cursor-pointer rounded-[3px] px-3 py-1.5 text-xs"
						}
					>
						{item.label}
					</button>
				))}
			</div>

			{tab === "overview" ? (
				<OverviewTab
					repo={repo}
					loading={repoState.loading}
					componentCount={componentState.data?.total ?? 0}
					assetCount={assetState.data?.total ?? 0}
				/>
			) : null}

			{tab === "browse" ? <BrowseTab repoName={name} repo={repo} version={version} /> : null}

			{tab === "components" ? (
				<ComponentsTab
					repo={repo}
					loading={componentState.loading}
					items={componentState.data?.items ?? []}
					total={componentState.data?.total ?? 0}
				/>
			) : null}

			{tab === "assets" ? (
				<AssetsTab
					loading={assetState.loading}
					items={assetState.data?.items ?? []}
					total={assetState.data?.total ?? 0}
				/>
			) : null}

			{tab === "settings" && repo ? (
				<SettingsTab
					repo={repo}
					repositories={allRepos.data?.items ?? []}
					onSaved={() => {
						reloadAll();
						toast.success("仓库设置已保存");
					}}
					onDeleted={() => {
						window.location.assign("/repositories");
					}}
				/>
			) : null}

			{tab === "settings" && !repo ? <Skeleton className="h-64 w-full" /> : null}
		</AppShell>
	);
}

function OverviewTab({
	repo,
	loading,
	componentCount,
	assetCount,
}: {
	repo?: Repository;
	loading: boolean;
	componentCount: number;
	assetCount: number;
}) {
	if (!repo) {
		return <Skeleton className="h-64 w-full" />;
	}
	const origin = typeof window === "undefined" ? "" : window.location.origin;
	const proxyURL = `${origin}/repository/${repo.name}`;
	return (
		<div className="space-y-3">
			<div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-4">
				<StatTile label="组件版本" value={loading ? "—" : formatNumber(componentCount)} hint="模块版本数量" />
				<StatTile label="资产" value={loading ? "—" : formatNumber(assetCount)} hint="info / mod / zip 与缓存元数据" />
				<StatTile label="存储占用" value={formatBytes(repo.stats.bytes)} hint="内容寻址 blob 大小" />
				<StatTile
					label="最近更新"
					value={relativeTime(repo.stats.lastUpdated)}
					hint={formatDateTime(repo.stats.lastUpdated)}
				/>
			</div>

			<div className="grid grid-cols-1 gap-3 xl:grid-cols-2">
				<Card className="gap-0 py-0">
					<div className="border-b px-4 py-3">
						<h2 className="text-sm font-semibold">仓库配置</h2>
						<p className="text-muted-foreground mt-0.5 text-[11px]">当前生效的协议参数</p>
					</div>
					<dl className="divide-y text-sm">
						<ConfigRow label="格式" value={<span className="font-mono text-xs">{repo.format}</span>} />
						<ConfigRow label="Recipe" value={<span className="font-mono text-xs">{repo.recipe}</span>} />
						<ConfigRow label="类型" value={repositoryTypeLabel(repo.type)} />
						<ConfigRow label="在线" value={repo.online ? "是" : "否"} />
						{repo.type === "hosted" ? (
							<ConfigRow label="写入策略" value={<span className="font-mono text-xs">{repo.hosted?.writePolicy}</span>} />
						) : null}
						{repo.type === "proxy" ? (
							<>
								<ConfigRow label="上游地址" value={<span className="font-mono text-xs">{repo.proxy?.remoteUrl}</span>} />
								<ConfigRow
									label="缓存有效期"
									value={`内容 ${repo.proxy?.contentMaxAgeMinutes} 分钟 · 元数据 ${repo.proxy?.metadataMaxAgeMinutes} 分钟`}
								/>
								<ConfigRow label="自动熔断" value={repo.proxy?.autoBlock ? "开启" : "关闭"} />
							</>
						) : null}
						{repo.type === "group" ? (
							<ConfigRow
								label="成员"
								value={<span className="font-mono text-xs">{repo.group?.memberNames.join(" → ") || "—"}</span>}
							/>
						) : null}
						<ConfigRow label="创建时间" value={formatDateTime(repo.createdAt)} />
					</dl>
				</Card>

				<Card className="gap-0 py-0">
					<div className="border-b px-4 py-3">
						<h2 className="text-sm font-semibold">客户端使用</h2>
						<p className="text-muted-foreground mt-0.5 text-[11px]">将仓库地址加入 GOPROXY</p>
					</div>
					<div className="space-y-4 p-4">
						<div>
							<Label className="text-[11px]">仓库地址</Label>
							<div className="mt-1.5 flex items-center gap-2">
								<code className="bg-muted flex-1 overflow-x-auto rounded-sm border px-2.5 py-2 font-mono text-[11px] whitespace-nowrap">
									{proxyURL}
								</code>
								<Button
									variant="outline"
									size="icon"
									className="size-8 shrink-0"
									aria-label="复制仓库地址"
									onClick={() => void copyText(proxyURL).then(() => toast.success("已复制仓库地址"))}
								>
									<Copy className="size-3.5" />
								</Button>
							</div>
							<pre className="bg-muted/60 mt-2 overflow-x-auto rounded-sm border p-2.5 font-mono text-[11px] leading-relaxed">
{clientCommands(repo.format, proxyURL)}
							</pre>
						</div>

						{repo.type === "hosted" ? (
							<div>
								<Label className="text-[11px]">上传文件</Label>
								<pre className="bg-muted/60 mt-1.5 overflow-x-auto rounded-sm border p-2.5 font-mono text-[11px] leading-relaxed">
{`curl -u admin:admin123 -X PUT \\
  --data-binary @artifact.bin \\
  ${proxyURL}/path/to/artifact.bin`}
								</pre>
								<p className="text-muted-foreground mt-1 text-[11px]">
									hosted 仓库接受对应格式的原始文件；npm / PyPI / Cargo / Docker 等格式也支持各自的发布协议。
								</p>
							</div>
						) : null}

						{repo.type === "proxy" && repo.proxyState ? (
							<div>
								<Label className="text-[11px]">上游健康</Label>
								<div className="text-muted-foreground mt-1.5 space-y-1 text-[11px]">
									<p>
										失败次数：<span className="text-foreground font-mono">{repo.proxyState.failCount}</span>
									</p>
									<p>
										最近成功：
										<span className="text-foreground font-mono">
											{formatDateTime(repo.proxyState.lastSuccessAt)}
										</span>
									</p>
									{repo.proxyState.lastError ? (
										<p className="text-destructive">
											最近错误：{repo.proxyState.lastError}
											{repo.proxyState.blockedUntil
												? ` · 熔断至 ${formatDateTime(repo.proxyState.blockedUntil)}`
												: ""}
										</p>
									) : null}
								</div>
							</div>
						) : null}
					</div>
				</Card>
			</div>
		</div>
	);
}

function ConfigRow({ label, value }: { label: string; value: React.ReactNode }) {
	return (
		<div className="flex items-start justify-between gap-4 px-4 py-2.5">
			<dt className="text-muted-foreground text-xs">{label}</dt>
			<dd className="max-w-[65%] text-right text-xs">{value}</dd>
		</div>
	);
}

function ComponentsTab({
	repo,
	loading,
	items,
	total,
}: {
	repo?: Repository;
	loading: boolean;
	items: Array<import("@/lib/api").Component>;
	total: number;
}) {
	const [query, setQuery] = useState("");
	const filtered = useMemo(() => {
		const normalized = query.trim().toLowerCase();
		if (!normalized) return items;
		return items.filter((item) => `${item.module} ${item.version}`.toLowerCase().includes(normalized));
	}, [items, query]);

	return (
		<Card className="gap-0 py-0">
			<div className="flex flex-wrap items-center gap-2 border-b px-3 py-2.5">
				<div className="relative">
					<Input
						value={query}
						onChange={(event) => setQuery(event.target.value)}
						placeholder="搜索模块或版本…"
						className="h-8 w-64 text-xs"
					/>
				</div>
				<span className="text-muted-foreground ml-auto text-[11px]">
					{formatNumber(filtered.length)} / {formatNumber(total)} 个版本
				</span>
			</div>
			{repo?.type === "group" ? (
				<p className="text-muted-foreground border-b bg-muted/30 px-4 py-2 text-[11px]">
					分组仓库不保存自己的组件，请求会按成员顺序转发。请查看成员仓库的组件。
				</p>
			) : null}
			<div className="overflow-x-auto">
				<table className="w-full text-sm">
					<thead>
						<tr className="bg-muted/50 text-muted-foreground">
							<th className="px-4 py-2 text-left text-[11px] font-medium">模块</th>
							<th className="px-3 py-2 text-left text-[11px] font-medium">版本</th>
							<th className="px-3 py-2 text-right text-[11px] font-medium">资产</th>
							<th className="px-3 py-2 text-right text-[11px] font-medium">大小</th>
							<th className="px-3 py-2 text-right text-[11px] font-medium">更新时间</th>
							<th className="px-3 py-2 text-right text-[11px] font-medium">下载</th>
						</tr>
					</thead>
					<tbody>
						{loading ? (
							Array.from({ length: 4 }).map((_, index) => (
								<tr key={index} className="border-b last:border-0">
									<td className="px-4 py-3" colSpan={6}>
										<Skeleton className="h-5 w-full" />
									</td>
								</tr>
							))
						) : filtered.length === 0 ? (
							<tr>
								<td className="text-muted-foreground px-4 py-12 text-center text-sm" colSpan={6}>
									暂无组件版本
								</td>
							</tr>
						) : (
							filtered.map((component) => (
								<tr key={`${component.module}@${component.version}`} className="hover:bg-muted/50 border-b last:border-0">
									<td className="px-4 py-2.5">
										<div className="max-w-[320px] truncate font-mono text-xs" title={component.module}>{component.module}</div>
									</td>
									<td className="px-3 py-2.5 font-mono text-xs">{component.version}</td>
									<td className="px-3 py-2.5 text-right font-mono text-xs tabular-nums">{component.assets}</td>
									<td className="px-3 py-2.5 text-right font-mono text-xs tabular-nums">{formatBytes(component.bytes)}</td>
									<td className="text-muted-foreground px-3 py-2.5 text-right font-mono text-[11px] whitespace-nowrap">
										{relativeTime(component.updatedAt)}
									</td>
									<td className="px-3 py-2.5 text-right">
										{Object.values(component.downloads).length > 0 ? (
											<a
												href={Object.values(component.downloads)[0]}
												className="text-muted-foreground hover:text-foreground inline-flex items-center gap-1 text-[11px] underline-offset-4 hover:underline"
											>
												<Download className="size-3.5" />
												下载
											</a>
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
	);
}

function AssetsTab({
	loading,
	items,
	total,
}: {
	loading: boolean;
	items: Array<import("@/lib/api").Asset>;
	total: number;
}) {
	const [query, setQuery] = useState("");
	const filtered = useMemo(() => {
		const normalized = query.trim().toLowerCase();
		if (!normalized) return items;
		return items.filter((item) => `${item.path} ${item.module}`.toLowerCase().includes(normalized));
	}, [items, query]);

	return (
		<Card className="gap-0 py-0">
			<div className="flex flex-wrap items-center gap-2 border-b px-3 py-2.5">
				<Input
					value={query}
					onChange={(event) => setQuery(event.target.value)}
					placeholder="搜索资产路径…"
					className="h-8 w-64 text-xs"
				/>
				<span className="text-muted-foreground ml-auto text-[11px]">
					{formatNumber(filtered.length)} / {formatNumber(total)} 个资产
				</span>
			</div>
			<div className="overflow-x-auto">
				<table className="w-full text-sm">
					<thead>
						<tr className="bg-muted/50 text-muted-foreground">
							<th className="px-4 py-2 text-left text-[11px] font-medium">路径</th>
							<th className="px-3 py-2 text-left text-[11px] font-medium">类型</th>
							<th className="px-3 py-2 text-left text-[11px] font-medium">来源</th>
							<th className="px-3 py-2 text-right text-[11px] font-medium">大小</th>
							<th className="px-3 py-2 text-right text-[11px] font-medium">更新时间</th>
							<th className="px-3 py-2 text-right text-[11px] font-medium">下载</th>
						</tr>
					</thead>
					<tbody>
						{loading ? (
							Array.from({ length: 6 }).map((_, index) => (
								<tr key={index} className="border-b last:border-0">
									<td className="px-4 py-3" colSpan={6}>
										<Skeleton className="h-5 w-full" />
									</td>
								</tr>
							))
						) : filtered.length === 0 ? (
							<tr>
								<td className="text-muted-foreground px-4 py-12 text-center text-sm" colSpan={6}>
									暂无资产。通过 GOPROXY 拉取模块后会出现在这里。
								</td>
							</tr>
						) : (
							filtered.map((asset) => (
								<tr key={asset.path} className="hover:bg-muted/50 border-b last:border-0">
									<td className="px-4 py-2.5">
										<div className="max-w-[360px] truncate font-mono text-[11px]" title={asset.path}>{asset.path}</div>
									</td>
									<td className="px-3 py-2.5">
										<StatusPill tone={asset.kind === "PACKAGE" ? "info" : "neutral"}>
											{kindLabel(asset.kind)}
										</StatusPill>
									</td>
									<td className="text-muted-foreground px-3 py-2.5 text-[11px]">{sourceLabel(asset.source)}</td>
									<td className="px-3 py-2.5 text-right font-mono text-xs tabular-nums">{formatBytes(asset.size)}</td>
									<td className="text-muted-foreground px-3 py-2.5 text-right font-mono text-[11px] whitespace-nowrap">
										{relativeTime(asset.updatedAt)}
									</td>
									<td className="px-3 py-2.5 text-right">
										<a
											href={asset.url}
											className="text-muted-foreground hover:text-foreground inline-flex items-center gap-1 text-[11px] underline-offset-4 hover:underline"
										>
											<Download className="size-3.5" />
											下载
										</a>
									</td>
								</tr>
							))
						)}
					</tbody>
				</table>
			</div>
		</Card>
	);
}

function SettingsTab({
	repo,
	repositories,
	onSaved,
	onDeleted,
}: {
	repo: Repository;
	repositories: Repository[];
	onSaved: () => void;
	onDeleted: () => void;
}) {
	const [online, setOnline] = useState(repo.online);
	const [description, setDescription] = useState(repo.description);
	const [writePolicy, setWritePolicy] = useState<string>(repo.hosted?.writePolicy ?? "ALLOW");
	const [remoteUrl, setRemoteUrl] = useState(repo.proxy?.remoteUrl ?? "");
	const [contentTtl, setContentTtl] = useState(String(repo.proxy?.contentMaxAgeMinutes ?? 1440));
	const [metadataTtl, setMetadataTtl] = useState(String(repo.proxy?.metadataMaxAgeMinutes ?? 1440));
	const [autoBlock, setAutoBlock] = useState(repo.proxy?.autoBlock ?? true);
	const [remoteUsername, setRemoteUsername] = useState(repo.proxy?.remoteUsername ?? "");
	const [remotePassword, setRemotePassword] = useState("");
	const [remoteBearer, setRemoteBearer] = useState("");
	const [members, setMembers] = useState<string[]>(repo.group?.memberNames ?? []);
	const [saving, setSaving] = useState(false);
	const [deleteOpen, setDeleteOpen] = useState(false);
	const [deleting, setDeleting] = useState(false);

	useEffect(() => {
		setOnline(repo.online);
		setDescription(repo.description);
		setWritePolicy(repo.hosted?.writePolicy ?? "ALLOW");
		setRemoteUrl(repo.proxy?.remoteUrl ?? "");
		setContentTtl(String(repo.proxy?.contentMaxAgeMinutes ?? 1440));
		setMetadataTtl(String(repo.proxy?.metadataMaxAgeMinutes ?? 1440));
		setAutoBlock(repo.proxy?.autoBlock ?? true);
		setRemoteUsername(repo.proxy?.remoteUsername ?? "");
		setRemotePassword("");
		setRemoteBearer("");
		setMembers(repo.group?.memberNames ?? []);
	}, [repo]);

	const candidates = repositories.filter(
		(item) => item.name !== repo.name && item.format === repo.format && (item.type === "hosted" || item.type === "proxy"),
	);

	async function save() {
		const payload: RepositoryInput = updatePayload(repo, {
			online,
			description,
		});
		if (repo.type === "hosted") payload.hosted = { writePolicy };
		if (repo.type === "proxy") {
			payload.proxy = {
				remoteUrl: remoteUrl.trim(),
				contentMaxAgeMinutes: Number(contentTtl),
				metadataMaxAgeMinutes: Number(metadataTtl),
				autoBlock,
				remoteUsername,
				remotePassword,
				remoteBearerToken: remoteBearer,
			};
		}
		if (repo.type === "group") payload.group = { memberNames: members };
		setSaving(true);
		try {
			await api.updateRepository(repo.name, payload);
			onSaved();
		} catch (err) {
			toast.error("保存失败", { description: errorMessage(err) });
		} finally {
			setSaving(false);
		}
	}

	async function remove() {
		setDeleting(true);
		try {
			await api.deleteRepository(repo.name);
			toast.success("仓库已删除", { description: repo.name });
			onDeleted();
		} catch (err) {
			toast.error("删除失败", { description: errorMessage(err) });
		} finally {
			setDeleting(false);
		}
	}

	return (
		<div className="grid grid-cols-1 gap-3 xl:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]">
			<Card className="gap-0 py-0">
				<div className="border-b px-4 py-3">
					<h2 className="text-sm font-semibold">仓库设置</h2>
					<p className="text-muted-foreground mt-0.5 text-[11px]">修改后立即对协议请求生效</p>
				</div>
				<div className="divide-y">
					<SettingRow label="在线" description="离线仓库会拒绝所有协议请求。">
						<Switch checked={online} onCheckedChange={setOnline} aria-label="仓库在线状态" />
					</SettingRow>
					<SettingRow label="描述" description="仅供控制台展示。">
						<Input
							value={description}
							onChange={(event) => setDescription(event.target.value)}
							className="h-8 w-64 text-xs"
						/>
					</SettingRow>
					{repo.type === "hosted" ? (
						<SettingRow label="写入策略" description="ALLOW 允许覆盖同一模块版本，DENY 拒绝重复上传。">
							<Select value={writePolicy} onValueChange={setWritePolicy}>
								<SelectTrigger className="h-8 w-40 text-xs">
									<SelectValue />
								</SelectTrigger>
								<SelectContent>
									<SelectItem value="ALLOW">ALLOW</SelectItem>
									<SelectItem value="DENY">DENY</SelectItem>
								</SelectContent>
							</Select>
						</SettingRow>
					) : null}
					{repo.type === "proxy" ? (
						<>
							<SettingRow label="上游地址" description="GOPROXY 协议兼容的 http(s) 上游。">
								<Input
									value={remoteUrl}
									onChange={(event) => setRemoteUrl(event.target.value)}
									className="h-8 w-72 font-mono text-xs"
								/>
							</SettingRow>
							<SettingRow label="内容缓存" description="zip 等内容的缓存分钟数，-1 为永不过期。">
								<Input
									value={contentTtl}
									onChange={(event) => setContentTtl(event.target.value)}
									className="h-8 w-28 font-mono text-xs"
								/>
							</SettingRow>
							<SettingRow label="元数据缓存" description="info / mod / list / latest 的缓存分钟数。">
								<Input
									value={metadataTtl}
									onChange={(event) => setMetadataTtl(event.target.value)}
									className="h-8 w-28 font-mono text-xs"
								/>
							</SettingRow>
							<SettingRow label="自动熔断" description="上游连续失败时暂停请求并按指数退避恢复。">
								<Switch checked={autoBlock} onCheckedChange={setAutoBlock} aria-label="自动熔断" />
							</SettingRow>
							<SettingRow label="上游用户名" description="Basic 认证用户名；留空表示匿名。">
								<Input value={remoteUsername} onChange={(event) => setRemoteUsername(event.target.value)} className="h-8 w-56 font-mono text-xs" />
							</SettingRow>
							<SettingRow label="上游密码 / Token" description="密码用于 Basic；Token 用于 Bearer；留空保持现有值。">
								<div className="flex gap-2">
									<Input type="password" value={remotePassword} onChange={(event) => setRemotePassword(event.target.value)} placeholder="密码" className="h-8 w-40 text-xs" />
									<Input type="password" value={remoteBearer} onChange={(event) => setRemoteBearer(event.target.value)} placeholder="Bearer" className="h-8 w-40 text-xs" />
								</div>
							</SettingRow>
						</>
					) : null}
					{repo.type === "group" ? (
						<SettingRow label="成员仓库" description="按列表顺序依次查找，不支持嵌套分组。">
							<div className="space-y-1.5">
								{candidates.map((candidate) => {
									const checked = members.includes(candidate.name);
									return (
										<label
											key={candidate.name}
											className="hover:bg-muted/50 flex cursor-pointer items-center gap-2 rounded-sm px-1.5 py-1 text-xs"
										>
											<input
												type="checkbox"
												checked={checked}
												onChange={() =>
													setMembers((current) =>
														checked ? current.filter((item) => item !== candidate.name) : [...current, candidate.name],
													)
												}
											/>
											<span className="font-mono">{candidate.name}</span>
											<StatusPill tone={TYPE_TONES[candidate.type] ?? "neutral"}>
												{repositoryTypeLabel(candidate.type)}
											</StatusPill>
										</label>
									);
								})}
							</div>
						</SettingRow>
					) : null}
				</div>
				<div className="flex items-center justify-end gap-2 border-t px-4 py-3">
					<Button variant="outline" size="sm" onClick={() => window.location.reload()}>
						重置
					</Button>
					<Button size="sm" isLoading={saving} onClick={() => void save()}>
						<Save className="size-3.5" />
						保存设置
					</Button>
				</div>
			</Card>

			<Card className="border-destructive/30 gap-0 py-0">
				<div className="border-b px-4 py-3">
					<h2 className="text-destructive text-sm font-semibold">危险操作</h2>
					<p className="text-muted-foreground mt-0.5 text-[11px]">删除后不可恢复</p>
				</div>
				<div className="space-y-3 p-4">
					<p className="text-muted-foreground text-xs leading-relaxed">
						删除会移除该仓库的全部组件、资产与缓存 blob。若仓库被分组引用，需要先从分组中移除。
					</p>
					<Button variant="destructive" size="sm" onClick={() => setDeleteOpen(true)}>
						<Trash2 className="size-3.5" />
						删除仓库
					</Button>
				</div>
			</Card>

			<Dialog open={deleteOpen} onOpenChange={setDeleteOpen}>
				<DialogContent className="sm:max-w-md">
					<DialogHeader>
						<DialogTitle>删除仓库</DialogTitle>
						<DialogDescription>
							确认删除 <span className="text-foreground font-mono">{repo.name}</span>？该操作不可撤销。
						</DialogDescription>
					</DialogHeader>
					<DialogFooter>
						<Button variant="outline" onClick={() => setDeleteOpen(false)}>
							取消
						</Button>
						<Button variant="destructive" isLoading={deleting} onClick={() => void remove()}>
							确认删除
						</Button>
					</DialogFooter>
				</DialogContent>
			</Dialog>
		</div>
	);
}

function SettingRow({
	label,
	description,
	children,
}: {
	label: string;
	description: string;
	children: React.ReactNode;
}) {
	return (
		<div className="flex flex-wrap items-center justify-between gap-3 px-4 py-3">
			<div className="min-w-0">
				<p className="text-xs font-medium">{label}</p>
				<p className="text-muted-foreground mt-0.5 text-[11px]">{description}</p>
			</div>
			<div>{children}</div>
		</div>
	);
}

function BrowseTab({ repoName, repo, version }: { repoName: string; repo?: Repository; version: number }) {
	const root = useApi(() => api.browse(repoName, ""), [repoName, version]);
	const [expanded, setExpanded] = useState<Set<string>>(new Set());
	const [children, setChildren] = useState<Record<string, BrowseEntry[]>>({});
	const [loadingPaths, setLoadingPaths] = useState<Record<string, boolean>>({});
	const [error, setError] = useState("");

	useEffect(() => {
		setExpanded(new Set());
		setChildren({});
		setError("");
	}, [repoName, version]);

	async function toggle(entry: BrowseEntry) {
		const next = new Set(expanded);
		if (next.has(entry.path)) {
			next.delete(entry.path);
			setExpanded(next);
			return;
		}
		next.add(entry.path);
		setExpanded(next);
		if (children[entry.path]) return;
		setLoadingPaths((current) => ({ ...current, [entry.path]: true }));
		try {
			const data = await api.browse(repoName, entry.path);
			setChildren((current) => ({ ...current, [entry.path]: data.entries }));
		} catch (err) {
			setError(errorMessage(err));
		} finally {
			setLoadingPaths((current) => ({ ...current, [entry.path]: false }));
		}
	}

	function renderEntries(entries: BrowseEntry[], depth: number): React.ReactNode {
		return entries.map((entry) => (
			<div key={entry.path}>
				{entry.type === "directory" ? (
					<button
						type="button"
						onClick={() => void toggle(entry)}
						aria-expanded={expanded.has(entry.path)}
						className="hover:bg-muted/50 flex w-full cursor-pointer items-center gap-2 border-b px-3 py-2 text-left transition-colors"
						style={{ paddingLeft: 12 + depth * 18 }}
					>
						{expanded.has(entry.path) ? (
							<ChevronDown className="text-muted-foreground size-3.5 shrink-0" />
						) : (
							<ChevronRight className="text-muted-foreground size-3.5 shrink-0" />
						)}
						{expanded.has(entry.path) ? (
							<FolderOpen className="text-chart-warning size-4 shrink-0" />
						) : (
							<Folder className="text-chart-warning size-4 shrink-0" />
						)}
						<span className="truncate font-mono text-xs">{entry.name}</span>
						<span className="text-muted-foreground ml-auto shrink-0 text-[11px]">
							{entry.count ?? 0} 项 · {formatBytes(entry.size ?? 0)}
						</span>
					</button>
				) : (
					<div
						className="hover:bg-muted/50 flex items-center gap-2 border-b px-3 py-2 transition-colors"
						style={{ paddingLeft: 12 + depth * 18 + 18 }}
					>
						<File className="text-muted-foreground size-4 shrink-0" />
						<span className="max-w-[45%] truncate font-mono text-xs" title={entry.path}>{entry.name}</span>
						<StatusPill tone="neutral">{kindLabel(entry.kind ?? "file")}</StatusPill>
						<span className="text-muted-foreground ml-auto hidden shrink-0 text-[11px] sm:inline">
							{formatBytes(entry.size ?? 0)} · {relativeTime(entry.updatedAt)}
						</span>
						{entry.url ? (
							<a
								href={entry.url}
								className="text-muted-foreground hover:text-foreground shrink-0"
								aria-label={`下载 ${entry.name}`}
							>
								<Download className="size-3.5" />
							</a>
						) : null}
					</div>
				)}
				{entry.type === "directory" && expanded.has(entry.path) ? (
					loadingPaths[entry.path] ? (
						<div className="text-muted-foreground border-b px-3 py-2 text-[11px]" style={{ paddingLeft: 48 + depth * 18 }}>
							正在加载…
						</div>
					) : children[entry.path] && children[entry.path].length > 0 ? (
						renderEntries(children[entry.path], depth + 1)
					) : (
						<div className="text-muted-foreground border-b px-3 py-2 text-[11px]" style={{ paddingLeft: 48 + depth * 18 }}>
							空目录
						</div>
					)
				) : null}
			</div>
		));
	}

	return (
		<Card className="gap-0 py-0">
			<div className="flex flex-wrap items-center justify-between gap-2 border-b px-4 py-3">
				<div>
					<h2 className="text-sm font-semibold">文件树</h2>
					<p className="text-muted-foreground mt-0.5 text-[11px]">
						按目录懒加载，点击文件夹展开；分组仓库会聚合成员内容。
					</p>
				</div>
				<Button
					variant="outline"
					size="sm"
					className="h-7 text-xs"
					onClick={() => {
						setChildren({});
						setExpanded(new Set());
						root.reload();
					}}
				>
					<RefreshCw className="size-3.5" />
					刷新
				</Button>
			</div>
			{repo?.type === "group" ? (
				<p className="text-muted-foreground border-b bg-muted/30 px-4 py-2 text-[11px]">
					当前为分组仓库，文件树由在线成员（{repo.group?.memberNames.join(" → ") || "—"}）聚合而来。
				</p>
			) : null}
			{error ? <p className="text-destructive border-b px-4 py-2 text-xs">{error}</p> : null}
			<div>
				{root.loading ? (
					Array.from({ length: 5 }).map((_, index) => (
						<div key={index} className="border-b px-4 py-3">
							<Skeleton className="h-4 w-full" />
						</div>
					))
				) : root.error ? (
					<p className="text-destructive px-4 py-8 text-center text-sm">{root.error}</p>
				) : (root.data?.entries ?? []).length === 0 ? (
					<div className="text-muted-foreground flex flex-col items-center gap-2 px-4 py-12 text-center text-sm">
						<Folder className="size-6 opacity-50" />
						仓库还没有任何文件
					</div>
				) : (
					renderEntries(root.data?.entries ?? [], 0)
				)}
			</div>
		</Card>
	);
}

function clientCommands(format: string, url: string): string {
	switch (format) {
		case "go":
			return `go env -w GOPROXY=${url},direct\ngo env -w GOSUMDB=off   # 私有模块需要`;
		case "maven":
			return `<!-- settings.xml -->\n<url>${url}</url>\nmvn dependency:get -DremoteRepositories=kkrepo::default::${url}`;
		case "npm":
			return `npm config set registry ${url}\nnpm publish --registry ${url}`;
		case "pypi":
			return `pip config set global.index-url ${url}/simple\ntwine upload --repository-url ${url} dist/*`;
		case "helm":
			return `helm repo add kkrepo ${url}\nhelm push my-chart-0.1.0.tgz oci://...`;
		case "cargo":
			return `# .cargo/config.toml\n[registries.kkrepo]\nindex = "${url}/index"\n\ncargo publish --registry kkrepo`;
		case "docker":
			return `docker login ${url.replace(/^https?:\/\//, "").split("/")[0]}\ndocker pull ${url.replace(/^https?:\/\//, "")}/library/alpine:latest`;
		case "gitlfs":
			return `git config lfs.url ${url}\ngit lfs push --all origin`;
		case "pub":
			return `export PUB_HOSTED_URL=${url}\ndart pub publish`;
		case "swift":
			return `swift package-registry set ${url}`;
		case "conda":
			return `conda config --add channels ${url}/main\nconda install <package>`;
		case "apt":
			return `deb [trusted=yes] ${url} stable main\napt-get update`;
		case "alpine":
			return `echo "${url}/v3.20/main" >> /etc/apk/repositories`;
		case "yum":
			return `# /etc/yum.repos.d/kkrepo.repo\n[kkrepo]\nbaseurl=${url}`;
		default:
			return `curl -u admin:admin123 -O ${url}/path/to/file`;
	}
}
