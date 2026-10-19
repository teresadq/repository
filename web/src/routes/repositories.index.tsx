import { Link, createFileRoute } from "@tanstack/react-router";
import {
	Boxes,
	Copy,
	MoreHorizontal,
	Plus,
	RefreshCw,
	Search,
	Trash2,
} from "lucide-react";
import { useMemo, useState } from "react";
import { toast } from "sonner";

import { AppShell } from "@/components/appShell";
import { StatusPill, type StatusTone } from "@/components/statusPill";
import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuSeparator,
	DropdownMenuTrigger,
} from "@/components/ui/dropdownMenu";
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
import { api, type Recipe, type Repository, type RepositoryInput } from "@/lib/api";
import { copyText, formatBytes, formatNumber, relativeTime, repositoryTypeLabel } from "@/lib/format";
import { errorMessage, updatePayload } from "@/lib/repository";
import { useApi } from "@/lib/useApi";

export const Route = createFileRoute("/repositories/")({
	component: RepositoriesPage,
});

const TYPE_TONES: Record<string, StatusTone> = {
	hosted: "neutral",
	proxy: "info",
	group: "purple",
};

function RepositoriesPage() {
	const { data, loading, error, reload } = useApi(() => api.repositories(), []);
	const recipes = useApi(() => api.recipes(), []);
	const [query, setQuery] = useState("");
	const [typeFilter, setTypeFilter] = useState("all");
	const [createOpen, setCreateOpen] = useState(false);
	const [deleteTarget, setDeleteTarget] = useState<Repository | null>(null);
	const [busy, setBusy] = useState<string | null>(null);

	const repositories = data?.items ?? [];
	const filtered = useMemo(() => {
		const normalized = query.trim().toLowerCase();
		return repositories.filter((repo) => {
			if (typeFilter !== "all" && repo.type !== typeFilter) return false;
			if (!normalized) return true;
			return `${repo.name} ${repo.description} ${repo.proxy?.remoteUrl ?? ""}`
				.toLowerCase()
				.includes(normalized);
		});
	}, [repositories, query, typeFilter]);

	async function toggleOnline(repo: Repository, online: boolean) {
		setBusy(repo.name);
		try {
			await api.updateRepository(repo.name, updatePayload(repo, { online }));
			toast.success(online ? "仓库已上线" : "仓库已下线", { description: repo.name });
			reload();
		} catch (err) {
			toast.error("更新失败", { description: errorMessage(err) });
		} finally {
			setBusy(null);
		}
	}

	async function remove(repo: Repository) {
		setBusy(repo.name);
		try {
			await api.deleteRepository(repo.name);
			toast.success("仓库已删除", { description: repo.name });
			setDeleteTarget(null);
			reload();
		} catch (err) {
			toast.error("删除失败", { description: errorMessage(err) });
		} finally {
			setBusy(null);
		}
	}

	return (
		<AppShell>
			<div className="mb-5 flex flex-wrap items-end justify-between gap-3">
				<div>
					<h1 className="text-xl font-semibold tracking-tight">仓库列表</h1>
					<p className="text-muted-foreground mt-1 text-sm">
						管理 Maven、npm、PyPI、Go、Helm、Cargo、Docker、Git LFS 等格式的 hosted、proxy 与 group 仓库，客户端通过{" "}
						<span className="font-mono text-xs">/repository/&lt;name&gt;</span> 访问。
					</p>
				</div>
				<div className="flex items-center gap-2">
					<Button variant="outline" size="sm" className="h-8" onClick={reload}>
						<RefreshCw className="size-3.5" />
						刷新
					</Button>
					<Button size="sm" className="h-8" onClick={() => setCreateOpen(true)}>
						<Plus className="size-3.5" />
						新建仓库
					</Button>
				</div>
			</div>

			<div className="bg-card rounded-sm border shadow-sm">
				<div className="flex flex-wrap items-center gap-2 border-b px-3 py-2.5">
					<div className="relative">
						<Search className="text-muted-foreground pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2" />
						<Input
							value={query}
							onChange={(event) => setQuery(event.target.value)}
							placeholder="搜索仓库名称、描述或上游地址…"
							className="h-8 w-64 pl-8 text-xs"
						/>
					</div>
					<Select value={typeFilter} onValueChange={setTypeFilter}>
						<SelectTrigger className="h-8 w-32 text-xs">
							<SelectValue placeholder="全部类型" />
						</SelectTrigger>
						<SelectContent>
							<SelectItem value="all">全部类型</SelectItem>
							<SelectItem value="hosted">本地</SelectItem>
							<SelectItem value="proxy">代理</SelectItem>
							<SelectItem value="group">分组</SelectItem>
						</SelectContent>
					</Select>
					<span className="text-muted-foreground ml-auto text-[11px]">
						{formatNumber(filtered.length)} / {formatNumber(repositories.length)} 个仓库
					</span>
				</div>

				<div className="overflow-x-auto">
					<table className="w-full text-sm">
						<thead>
							<tr className="bg-muted/50 text-muted-foreground">
								<th className="px-4 py-2 text-left text-[11px] font-medium">仓库</th>
								<th className="px-3 py-2 text-left text-[11px] font-medium">类型</th>
								<th className="px-3 py-2 text-left text-[11px] font-medium">状态</th>
								<th className="px-3 py-2 text-right text-[11px] font-medium">组件</th>
								<th className="px-3 py-2 text-right text-[11px] font-medium">资产</th>
								<th className="px-3 py-2 text-right text-[11px] font-medium">存储</th>
								<th className="px-3 py-2 text-right text-[11px] font-medium">最近更新</th>
								<th className="px-3 py-2 text-right text-[11px] font-medium">操作</th>
							</tr>
						</thead>
						<tbody>
							{loading ? (
								Array.from({ length: 4 }).map((_, index) => (
									<tr key={index} className="border-b last:border-0">
										<td className="px-4 py-3" colSpan={8}>
											<Skeleton className="h-5 w-full" />
										</td>
									</tr>
								))
							) : error ? (
								<tr>
									<td className="text-destructive px-4 py-10 text-center text-sm" colSpan={8}>
										{error}
									</td>
								</tr>
							) : filtered.length === 0 ? (
								<tr>
									<td className="text-muted-foreground px-4 py-12 text-center text-sm" colSpan={8}>
										<div className="flex flex-col items-center gap-2">
											<Boxes className="size-6 opacity-50" />
											没有匹配的仓库
										</div>
									</td>
								</tr>
							) : (
								filtered.map((repo) => (
									<tr key={repo.name} className="hover:bg-muted/50 border-b last:border-0">
										<td className="px-4 py-2.5">
											<Link
												to="/repositories/$name"
												params={{ name: repo.name }}
												className="hover:text-foreground font-mono text-[13px] font-medium underline-offset-4 hover:underline"
											>
												{repo.name}
											</Link>
											<p className="text-muted-foreground mt-0.5 max-w-[320px] truncate text-[11px]">
												{repo.type === "proxy"
													? repo.proxy?.remoteUrl
													: repo.type === "group"
														? `成员：${repo.group?.memberNames.join(", ") || "—"}`
														: repo.description || "本地上传仓库"}
											</p>
										</td>
										<td className="px-3 py-2.5">
											<div className="flex flex-wrap items-center gap-1">
												<StatusPill tone="neutral">{repo.format}</StatusPill>
												<StatusPill tone={TYPE_TONES[repo.type] ?? "neutral"} dot>
													{repositoryTypeLabel(repo.type)}
												</StatusPill>
											</div>
										</td>
										<td className="px-3 py-2.5">
											<div className="flex items-center gap-2">
												<Switch
													checked={repo.online}
													disabled={busy === repo.name}
													onCheckedChange={(checked) => void toggleOnline(repo, checked)}
													aria-label={`切换 ${repo.name} 在线状态`}
												/>
												<span className="text-muted-foreground text-[11px]">
													{repo.online ? "在线" : "离线"}
												</span>
											</div>
										</td>
										<td className="px-3 py-2.5 text-right font-mono text-xs tabular-nums">
											{formatNumber(repo.stats.components)}
										</td>
										<td className="px-3 py-2.5 text-right font-mono text-xs tabular-nums">
											{formatNumber(repo.stats.assets)}
										</td>
										<td className="px-3 py-2.5 text-right font-mono text-xs tabular-nums">
											{formatBytes(repo.stats.bytes)}
										</td>
										<td className="text-muted-foreground px-3 py-2.5 text-right font-mono text-[11px]">
											{relativeTime(repo.stats.lastUpdated)}
										</td>
										<td className="px-3 py-2.5 text-right">
											<DropdownMenu>
												<DropdownMenuTrigger asChild>
													<Button
														variant="ghost"
														size="icon"
														className="text-muted-foreground size-7"
														aria-label={`${repo.name} 操作`}
													>
														<MoreHorizontal className="size-4" />
													</Button>
												</DropdownMenuTrigger>
												<DropdownMenuContent align="end" className="w-44">
													<DropdownMenuItem asChild>
														<Link to="/repositories/$name" params={{ name: repo.name }}>
															查看详情
														</Link>
													</DropdownMenuItem>
													<DropdownMenuItem
														onClick={() => {
															void copyText(`${window.location.origin}${repo.url}`).then(() =>
																toast.success("GOPROXY 地址已复制", {
																	description: `${window.location.origin}${repo.url}`,
																}),
															);
														}}
													>
														<Copy className="size-3.5" />
														复制仓库地址
													</DropdownMenuItem>
													<DropdownMenuSeparator />
													<DropdownMenuItem variant="destructive" onClick={() => setDeleteTarget(repo)}>
														<Trash2 className="size-3.5" />
														删除仓库
													</DropdownMenuItem>
												</DropdownMenuContent>
											</DropdownMenu>
										</td>
									</tr>
								))
							)}
						</tbody>
					</table>
				</div>
			</div>

			<CreateRepositoryDialog
				open={createOpen}
				onOpenChange={setCreateOpen}
				repositories={repositories}
				recipes={recipes.data?.items ?? []}
				onCreated={() => {
					setCreateOpen(false);
					reload();
				}}
			/>

			<Dialog open={!!deleteTarget} onOpenChange={(open) => !open && setDeleteTarget(null)}>
				<DialogContent className="sm:max-w-md">
					<DialogHeader>
						<DialogTitle>删除仓库</DialogTitle>
						<DialogDescription>
							将删除 <span className="text-foreground font-mono">{deleteTarget?.name}</span> 及其全部资产与缓存。
							该操作不可撤销。
						</DialogDescription>
					</DialogHeader>
					<DialogFooter>
						<Button variant="outline" onClick={() => setDeleteTarget(null)}>
							取消
						</Button>
						<Button
							variant="destructive"
							isLoading={busy === deleteTarget?.name}
							onClick={() => deleteTarget && void remove(deleteTarget)}
						>
							确认删除
						</Button>
					</DialogFooter>
				</DialogContent>
			</Dialog>
		</AppShell>
	);
}

const REMOTE_DEFAULTS: Record<string, string> = {
	maven: "https://repo1.maven.org/maven2",
	npm: "https://registry.npmjs.org",
	pypi: "https://pypi.org",
	go: "https://proxy.golang.org",
	helm: "https://charts.helm.sh/stable",
	cargo: "https://index.crates.io",
	pub: "https://pub.dev",
	docker: "https://registry-1.docker.io",
	conda: "https://conda.anaconda.org",
	swift: "https://github.com",
	apt: "https://deb.debian.org/debian",
	alpine: "https://dl-cdn.alpinelinux.org/alpine",
	yum: "https://example.com/yum",
	raw: "https://example.com/files",
};

function CreateRepositoryDialog({
	open,
	onOpenChange,
	repositories,
	recipes,
	onCreated,
}: {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	repositories: Repository[];
	recipes: Recipe[];
	onCreated: () => void;
}) {
	const [name, setName] = useState("");
	const [recipe, setRecipe] = useState("maven-proxy");
	const [description, setDescription] = useState("");
	const [remoteUrl, setRemoteUrl] = useState(REMOTE_DEFAULTS.maven);
	const [contentTtl, setContentTtl] = useState("1440");
	const [metadataTtl, setMetadataTtl] = useState("1440");
	const [writePolicy, setWritePolicy] = useState("ALLOW");
	const [remoteUsername, setRemoteUsername] = useState("");
	const [remotePassword, setRemotePassword] = useState("");
	const [remoteBearer, setRemoteBearer] = useState("");
	const [members, setMembers] = useState<string[]>([]);
	const [saving, setSaving] = useState(false);

	const selectedFormat = recipes.find((item) => item.name === recipe)?.format ?? recipe.split("-")[0];
	const recipeType = recipe.endsWith("-group") ? "group" : recipe.endsWith("-proxy") ? "proxy" : "hosted";
	const candidates = repositories.filter(
		(repo) => repo.format === selectedFormat && (repo.type === "hosted" || repo.type === "proxy"),
	);

	function reset() {
		setName("");
		setRecipe("maven-proxy");
		setDescription("");
		setRemoteUrl(REMOTE_DEFAULTS.maven);
		setContentTtl("1440");
		setMetadataTtl("1440");
		setWritePolicy("ALLOW");
		setRemoteUsername("");
		setRemotePassword("");
		setRemoteBearer("");
		setMembers([]);
	}

	async function submit() {
		const payload: RepositoryInput = {
			name: name.trim(),
			recipe,
			description: description.trim(),
			online: true,
		};
		if (recipeType === "proxy") {
			payload.proxy = {
				remoteUrl: remoteUrl.trim(),
				contentMaxAgeMinutes: Number(contentTtl),
				metadataMaxAgeMinutes: Number(metadataTtl),
				autoBlock: true,
				remoteUsername: remoteUsername.trim(),
				remotePassword,
				remoteBearerToken: remoteBearer,
			};
		} else if (recipeType === "hosted") {
			payload.hosted = { writePolicy };
		} else {
			payload.group = { memberNames: members };
		}
		setSaving(true);
		try {
			await api.createRepository(payload);
			toast.success("仓库已创建", { description: payload.name });
			reset();
			onCreated();
		} catch (err) {
			toast.error("创建失败", { description: errorMessage(err) });
		} finally {
			setSaving(false);
		}
	}

	return (
		<Dialog
			open={open}
			onOpenChange={(next) => {
				if (!next) reset();
				onOpenChange(next);
			}}
		>
			<DialogContent className="sm:max-w-lg">
				<DialogHeader>
					<DialogTitle>新建 Go 仓库</DialogTitle>
					<DialogDescription>
						选择 recipe 并填写上游或成员配置，创建后即可通过{" "}
						<span className="font-mono">/repository/&lt;name&gt;</span> 使用。
					</DialogDescription>
				</DialogHeader>

				<div className="space-y-4">
					<div>
						<Label htmlFor="repo-name">仓库名称</Label>
						<Input
							id="repo-name"
							value={name}
							onChange={(event) => setName(event.target.value)}
							placeholder="例如 go-public"
							className="mt-1.5 h-9 font-mono"
							autoComplete="off"
						/>
						<p className="text-muted-foreground mt-1 text-[11px]">仅小写字母、数字、点、下划线和连字符。</p>
					</div>

					<div>
						<Label>Recipe</Label>
						<Select
							value={recipe}
							onValueChange={(value) => {
								setRecipe(value);
								const format = recipes.find((item) => item.name === value)?.format;
								if (format && REMOTE_DEFAULTS[format]) setRemoteUrl(REMOTE_DEFAULTS[format]);
							}}
						>
							<SelectTrigger className="mt-1.5 h-9 w-full">
								<SelectValue />
							</SelectTrigger>
							<SelectContent className="max-h-[320px]">
								{recipes.map((item) => (
									<SelectItem key={item.name} value={item.name}>
										{item.label} · <span className="font-mono text-xs">{item.name}</span>
									</SelectItem>
								))}
							</SelectContent>
						</Select>
					</div>

					<div>
						<Label htmlFor="repo-description">描述</Label>
						<Input
							id="repo-description"
							value={description}
							onChange={(event) => setDescription(event.target.value)}
							placeholder="可选"
							className="mt-1.5 h-9"
						/>
					</div>

					{recipeType === "proxy" ? (
						<>
							<div>
								<Label htmlFor="remote-url">上游地址</Label>
								<Input
									id="remote-url"
									value={remoteUrl}
									onChange={(event) => setRemoteUrl(event.target.value)}
									placeholder="https://proxy.golang.org"
									className="mt-1.5 h-9 font-mono text-xs"
								/>
							</div>
							<div className="grid grid-cols-2 gap-3">
								<div>
									<Label htmlFor="content-ttl">内容缓存（分钟）</Label>
									<Input
										id="content-ttl"
										value={contentTtl}
										onChange={(event) => setContentTtl(event.target.value)}
										className="mt-1.5 h-9 font-mono"
									/>
								</div>
								<div>
									<Label htmlFor="metadata-ttl">元数据缓存（分钟）</Label>
									<Input
										id="metadata-ttl"
										value={metadataTtl}
										onChange={(event) => setMetadataTtl(event.target.value)}
										className="mt-1.5 h-9 font-mono"
									/>
								</div>
							</div>
							<p className="text-muted-foreground -mt-2 text-[11px]">-1 表示永不过期；0 使用默认 1440 分钟。</p>
							<div className="grid grid-cols-3 gap-3">
								<div>
									<Label htmlFor="remote-user">上游用户名</Label>
									<Input id="remote-user" value={remoteUsername} onChange={(event) => setRemoteUsername(event.target.value)} className="mt-1.5 h-9 text-xs" placeholder="可选" />
								</div>
								<div>
									<Label htmlFor="remote-pass">上游密码</Label>
									<Input id="remote-pass" type="password" value={remotePassword} onChange={(event) => setRemotePassword(event.target.value)} className="mt-1.5 h-9 text-xs" placeholder="可选" />
								</div>
								<div>
									<Label htmlFor="remote-token">Bearer Token</Label>
									<Input id="remote-token" type="password" value={remoteBearer} onChange={(event) => setRemoteBearer(event.target.value)} className="mt-1.5 h-9 text-xs" placeholder="可选" />
								</div>
							</div>
						</>
					) : null}

					{recipeType === "hosted" ? (
						<div>
							<Label>写入策略</Label>
							<Select value={writePolicy} onValueChange={setWritePolicy}>
								<SelectTrigger className="mt-1.5 h-9 w-full">
									<SelectValue />
								</SelectTrigger>
								<SelectContent>
									<SelectItem value="ALLOW">ALLOW · 允许覆盖同版本</SelectItem>
									<SelectItem value="DENY">DENY · 拒绝重复版本</SelectItem>
								</SelectContent>
							</Select>
						</div>
					) : null}

					{recipeType === "group" ? (
						<div>
							<Label>成员仓库</Label>
							<div className="mt-1.5 space-y-1.5 rounded-sm border p-2.5">
								{candidates.length === 0 ? (
									<p className="text-muted-foreground text-xs">
										先创建 hosted 或 proxy 仓库，再加入分组。
									</p>
								) : (
									candidates.map((candidate) => {
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
															checked
																? current.filter((item) => item !== candidate.name)
																: [...current, candidate.name],
														)
													}
												/>
												<span className="font-mono">{candidate.name}</span>
												<StatusPill tone={TYPE_TONES[candidate.type] ?? "neutral"}>
													{repositoryTypeLabel(candidate.type)}
												</StatusPill>
											</label>
										);
									})
								)}
							</div>
							<p className="text-muted-foreground mt-1 text-[11px]">按勾选顺序依次查找，不可嵌套分组。</p>
						</div>
					) : null}
				</div>

				<DialogFooter>
					<Button variant="outline" onClick={() => onOpenChange(false)}>
						取消
					</Button>
					<Button isLoading={saving} disabled={!name.trim()} onClick={() => void submit()}>
						创建仓库
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}

