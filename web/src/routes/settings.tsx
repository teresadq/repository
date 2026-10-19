import { createFileRoute } from "@tanstack/react-router";
import { Check, Copy, Download, Info, Moon, Sun, Monitor, Upload } from "lucide-react";
import { useTheme } from "next-themes";
import { useEffect, useState } from "react";
import { toast } from "sonner";

import { AppShell } from "@/components/appShell";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { api } from "@/lib/api";
import { copyText } from "@/lib/format";
import { useApi } from "@/lib/useApi";
import { cn } from "@/lib/utils";

export const Route = createFileRoute("/settings")({
	component: SettingsPage,
});

const ENV_ROWS = [
	{ key: "KKREPO_ADDR", description: "HTTP 监听地址", fallback: ":8080" },
	{ key: "KKREPO_DATA", description: "元数据与 blob 数据目录", fallback: "./data" },
	{ key: "KKREPO_ADMIN_USER", description: "控制台管理员用户名", fallback: "admin" },
	{ key: "KKREPO_ADMIN_PASSWORD", description: "控制台管理员密码", fallback: "admin123" },
	{ key: "KKREPO_SESSION_SECRET", description: "会话 HMAC 密钥（留空则随机生成）", fallback: "（随机）" },
	{ key: "KKREPO_PUBLIC_URL", description: "对外访问地址，用于协议入口提示", fallback: "自动推断" },
];

const API_ROWS = [
	{ method: "GET", path: "/api/health", description: "健康检查（无需登录）" },
	{ method: "GET", path: "/api/overview", description: "概览指标与最近活动" },
	{ method: "GET", path: "/api/repositories", description: "仓库列表" },
	{ method: "POST", path: "/api/repositories", description: "创建仓库" },
	{ method: "PUT", path: "/api/repositories/{name}", description: "更新仓库设置" },
	{ method: "DELETE", path: "/api/repositories/{name}", description: "删除仓库" },
	{ method: "GET", path: "/api/repositories/{name}/components", description: "模块版本列表" },
	{ method: "GET", path: "/api/repositories/{name}/assets", description: "资产列表" },
	{ method: "GET", path: "/api/repositories/{name}/browse", description: "文件树（按目录懒加载）" },
	{ method: "GET", path: "/api/users | /api/roles | /api/groups", description: "用户、角色与用户组" },
	{ method: "GET", path: "/api/tokens", description: "API Token 管理" },
	{ method: "GET", path: "/api/audit | /api/logs", description: "审计日志与请求日志" },
	{ method: "GET", path: "/metrics", description: "Prometheus 指标" },
	{ method: "GET/HEAD", path: "/repository/{name}/...", description: "多格式制品协议入口" },
	{ method: "PUT/POST/PATCH/DELETE", path: "/repository/{name}/...", description: "各格式的原生发布/上传端点（需写权限）" },
];

function SettingsPage() {
	const { data } = useApi(() => api.status(), []);
	const [backupFile, setBackupFile] = useState<File | null>(null);
	const [backupMode, setBackupMode] = useState("merge");
	const [backupForce, setBackupForce] = useState(false);
	const [importing, setImporting] = useState(false);
	const origin = typeof window === "undefined" ? "" : window.location.origin;
	const proxyURL = `${origin}/repository/go-group`;

	return (
		<AppShell>
			<div className="mb-5">
				<h1 className="text-xl font-semibold tracking-tight">系统设置</h1>
				<p className="text-muted-foreground mt-1 text-sm">
					运行时配置由环境变量驱动；多格式仓库、用户与权限请进入对应页面。
				</p>
			</div>

			<div className="bg-muted/50 mb-3 flex items-start gap-2 rounded-sm border p-3">
				<Info className="text-muted-foreground mt-0.5 size-3.5 shrink-0" />
				<p className="text-muted-foreground text-[11px] leading-relaxed">
					当前实现：元数据保存在本地 JSON 文件，blob 使用内容寻址目录；已覆盖 16 种制品格式、RBAC 与可观测性。
					生产部署时建议将数据目录挂载到持久卷，并通过环境变量修改默认管理员密码。
				</p>
			</div>

			<div className="grid grid-cols-1 gap-3 xl:grid-cols-2">
				<Card className="gap-0 py-0">
					<CardHeader title="Go 客户端配置（示例）" description="以 Go 分组仓库为例；其他格式在仓库详情页提供对应客户端命令。" />
					<div className="space-y-3 p-4">
						<div>
							<p className="text-muted-foreground mb-1 text-[11px]">GOPROXY 地址</p>
							<div className="flex items-center gap-2">
								<code className="bg-muted flex-1 overflow-x-auto rounded-sm border px-2.5 py-2 font-mono text-[11px] whitespace-nowrap">
									{proxyURL}
								</code>
								<CopyButton value={proxyURL} />
							</div>
						</div>
						<div>
							<p className="text-muted-foreground mb-1 text-[11px]">配置命令</p>
							<pre className="bg-muted/60 overflow-x-auto rounded-sm border p-2.5 font-mono text-[11px] leading-relaxed">
{`go env -w GOPROXY=${proxyURL},direct
go env -w GOSUMDB=off
go env -w GONOSUMDB=example.com/*`}
							</pre>
							<p className="text-muted-foreground mt-1 text-[11px]">
								仅当模块没有公共 checksum 记录时，才需要为私有域名关闭 sumdb。
							</p>
						</div>
					</div>
				</Card>

				<Card className="gap-0 py-0">
					<CardHeader title="外观" description="主题跟随 Bifrost 设计系统的语义色令牌。" />
					<ThemeSection />
				</Card>

				<Card className="gap-0 py-0">
					<CardHeader title="环境变量" description="修改后需要重启进程生效。" />
					<div className="overflow-x-auto">
						<table className="w-full text-sm">
							<thead>
								<tr className="bg-muted/50 text-muted-foreground">
									<th className="px-4 py-2 text-left text-[11px] font-medium">变量</th>
									<th className="px-3 py-2 text-left text-[11px] font-medium">说明</th>
									<th className="px-4 py-2 text-right text-[11px] font-medium">默认值</th>
								</tr>
							</thead>
							<tbody>
								{ENV_ROWS.map((row) => (
									<tr key={row.key} className="border-b last:border-0">
										<td className="px-4 py-2.5 font-mono text-[11px]">{row.key}</td>
										<td className="text-muted-foreground px-3 py-2.5 text-[11px]">{row.description}</td>
										<td className="text-muted-foreground px-4 py-2.5 text-right font-mono text-[11px]">{row.fallback}</td>
									</tr>
								))}
							</tbody>
						</table>
					</div>
				</Card>

				<Card className="gap-0 py-0">
					<CardHeader title="数据与备份" description="单节点文件存储的运行时信息。" />
					<dl className="divide-y text-sm">
						<InfoRow label="数据目录" value={data?.dataDir ?? "—"} />
						<InfoRow label="元数据文件" value="state.json" />
						<InfoRow label="Blob 目录" value="blobs/<sha256 前两位>/<sha256>" />
						<InfoRow label="备份方式" value="停止写入后复制整个数据目录" />
						<InfoRow label="控制台版本" value={data?.version ?? "—"} />
					</dl>
				</Card>
			</div>

			<Card className="mt-3 gap-0 py-0">
				<CardHeader title="备份与迁移" description="导出一致性归档，或从归档恢复/合并到当前实例。" />
				<div className="space-y-4 p-4">
					<div className="flex flex-wrap items-center gap-2">
						<Button variant="outline" size="sm" asChild>
							<a href="/api/backup/export">
								<Download className="size-3.5" />
								导出完整备份
							</a>
						</Button>
						<p className="text-muted-foreground text-[11px]">
							归档包含 state.json 与所有被引用 blob；导出期间写入会短暂阻塞。
						</p>
					</div>
					<div className="border-t pt-4">
						<div className="flex flex-wrap items-end gap-3">
							<div className="min-w-0 flex-1">
								<Label>选择备份归档</Label>
								<input
									type="file"
									accept=".gz,.tgz,.tar.gz"
									onChange={(event) => setBackupFile(event.target.files?.[0] ?? null)}
									className="text-muted-foreground mt-1.5 block w-full text-xs file:mr-3 file:cursor-pointer file:rounded-sm file:border file:bg-card file:px-2.5 file:py-1.5 file:text-xs"
								/>
							</div>
							<div>
								<Label>模式</Label>
								<select
									value={backupMode}
									onChange={(event) => setBackupMode(event.target.value)}
									className="bg-background focus-visible:border-primary mt-1.5 h-8 rounded-sm border px-2.5 text-xs outline-none"
								>
									<option value="merge">合并（仅新增仓库）</option>
									<option value="replace">替换（完整恢复）</option>
								</select>
							</div>
							{backupMode === "replace" ? (
								<label className="text-muted-foreground flex items-center gap-2 pb-1.5 text-xs">
									<input type="checkbox" checked={backupForce} onChange={(event) => setBackupForce(event.target.checked)} />
									确认覆盖当前数据
								</label>
							) : null}
							<Button
								size="sm"
								className="h-8"
								isLoading={importing}
								disabled={!backupFile || (backupMode === "replace" && !backupForce)}
								onClick={async () => {
									if (!backupFile) return;
									setImporting(true);
									try {
										const result = await api.uploadBackup(backupFile, backupMode, backupForce);
										toast.success("备份已导入", {
											description: `新增仓库 ${result.repositories}，跳过 ${result.skipped}，资产 ${result.assets}，blob ${result.blobs}`,
										});
										setTimeout(() => window.location.reload(), 800);
									} catch (err) {
										toast.error("导入失败", { description: err instanceof Error ? err.message : "未知错误" });
									} finally {
										setImporting(false);
									}
								}}
							>
								<Upload className="size-3.5" />
								导入
							</Button>
						</div>
						<p className="text-muted-foreground mt-2 text-[11px] leading-relaxed">
							合并模式只导入当前不存在的仓库，保留本机用户/角色/Token/审计；替换模式会先备份 state.json，再完整覆盖。
							CLI：<span className="font-mono">kkrepo-go export -out backup.tgz</span> /{" "}
							<span className="font-mono">kkrepo-go import -in backup.tgz -force</span>。
						</p>
					</div>
				</div>
			</Card>

			<Card className="mt-3 gap-0 py-0">
				<CardHeader title="HTTP API" description="控制台使用的 JSON API 与 Go 协议入口。" />
				<div className="overflow-x-auto">
					<table className="w-full text-sm">
						<thead>
							<tr className="bg-muted/50 text-muted-foreground">
								<th className="px-4 py-2 text-left text-[11px] font-medium">方法</th>
								<th className="px-3 py-2 text-left text-[11px] font-medium">路径</th>
								<th className="px-4 py-2 text-left text-[11px] font-medium">说明</th>
							</tr>
						</thead>
						<tbody>
							{API_ROWS.map((row) => (
								<tr key={`${row.method}-${row.path}`} className="border-b last:border-0">
									<td className="px-4 py-2.5">
										<span className="bg-secondary rounded px-1.5 py-0.5 font-mono text-[10px] font-medium">
											{row.method}
										</span>
									</td>
									<td className="px-3 py-2.5 font-mono text-[11px]">{row.path}</td>
									<td className="text-muted-foreground px-4 py-2.5 text-[11px]">{row.description}</td>
								</tr>
							))}
						</tbody>
					</table>
				</div>
			</Card>

			<div className="mt-4 flex justify-end">
				<Button
					variant="outline"
					size="sm"
					onClick={() =>
						void copyText(`GOPROXY=${proxyURL},direct`).then(() => toast.success("已复制 GOPROXY 配置"))
					}
				>
					<Copy className="size-3.5" />
					复制 GOPROXY 配置
				</Button>
			</div>
		</AppShell>
	);
}

function ThemeSection() {
	const { theme, setTheme } = useTheme();
	const [mounted, setMounted] = useState(false);
	useEffect(() => setMounted(true), []);
	const options = [
		{ value: "light", label: "浅色", icon: Sun },
		{ value: "dark", label: "深色", icon: Moon },
		{ value: "system", label: "跟随系统", icon: Monitor },
	];
	return (
		<div className="grid grid-cols-3 gap-2 p-4">
			{options.map((option) => {
				const Icon = option.icon;
				const active = mounted && theme === option.value;
				return (
					<button
						key={option.value}
						type="button"
						onClick={() => setTheme(option.value)}
						className={cn(
							"flex cursor-pointer flex-col items-center gap-2 rounded-sm border p-3 text-xs transition-colors",
							active ? "border-foreground/30 bg-secondary font-medium" : "hover:bg-muted/60 text-muted-foreground",
						)}
					>
						<Icon className="size-4" />
						{option.label}
						{active ? <Check className="text-chart-success size-3" /> : null}
					</button>
				);
			})}
		</div>
	);
}

function CardHeader({ title, description }: { title: string; description: string }) {
	return (
		<div className="border-b px-4 py-3">
			<h2 className="text-sm font-semibold">{title}</h2>
			<p className="text-muted-foreground mt-0.5 text-[11px]">{description}</p>
		</div>
	);
}

function InfoRow({ label, value }: { label: string; value: string }) {
	return (
		<div className="flex items-center justify-between gap-3 px-4 py-2.5">
			<dt className="text-muted-foreground text-xs">{label}</dt>
			<dd className="max-w-[65%] truncate text-right font-mono text-[11px]">{value}</dd>
		</div>
	);
}

function CopyButton({ value }: { value: string }) {
	return (
		<Button
			variant="outline"
			size="icon"
			className="size-8 shrink-0"
			aria-label="复制"
			onClick={() => void copyText(value).then(() => toast.success("已复制"))}
		>
			<Copy className="size-3.5" />
		</Button>
	);
}
