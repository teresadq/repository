import { createFileRoute } from "@tanstack/react-router";
import { FlaskConical, Plus, RefreshCw, ShieldCheck, Trash2 } from "lucide-react";
import { useState } from "react";
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
import { api, type ContentSelector } from "@/lib/api";
import { errorMessage } from "@/lib/repository";
import { useApi } from "@/lib/useApi";

export const Route = createFileRoute("/selectors")({
	component: SelectorsPage,
});

function SelectorsPage() {
	const [version, setVersion] = useState(0);
	const [editing, setEditing] = useState<ContentSelector | "new" | null>(null);
	const selectors = useApi(() => api.selectors(), [version]);
	const repositories = useApi(() => api.repositories(), []);
	const reload = () => setVersion((value) => value + 1);

	return (
		<AppShell>
			<div className="mb-5 flex flex-wrap items-end justify-between gap-3">
				<div>
					<h1 className="text-xl font-semibold tracking-tight">内容选择器</h1>
					<p className="text-muted-foreground mt-1 text-sm">
						用表达式限制角色在仓库内部可访问的路径。权限格式：{" "}
						<span className="font-mono text-xs">nexus:repository-content-selector:&lt;format&gt;:&lt;repo&gt;:&lt;selector&gt;:&lt;action&gt;</span>
					</p>
				</div>
				<div className="flex gap-2">
					<Button variant="outline" size="sm" className="h-8" onClick={reload}>
						<RefreshCw className="size-3.5" />
						刷新
					</Button>
					<Button size="sm" className="h-8" onClick={() => setEditing("new")}>
						<Plus className="size-3.5" />
						新建选择器
					</Button>
				</div>
			</div>

			<Card className="gap-0 py-0">
				<div className="border-b px-4 py-3">
					<h2 className="text-sm font-semibold">选择器列表</h2>
					<p className="text-muted-foreground mt-0.5 text-[11px]">
						支持 path / format / repository 字段，== !=、startsWith / endsWith / contains / matches，以及 &amp;&amp; || ! 与括号。
					</p>
				</div>
				<div className="overflow-x-auto">
					<table className="w-full text-sm">
						<thead>
							<tr className="bg-muted/50 text-muted-foreground">
								<Th>名称</Th><Th>说明</Th><Th>表达式</Th><Th className="text-right">操作</Th>
							</tr>
						</thead>
						<tbody>
							{(selectors.data?.items ?? []).length === 0 ? (
								<tr><td className="text-muted-foreground px-4 py-10 text-center text-sm" colSpan={4}>暂无内容选择器</td></tr>
							) : (
								selectors.data?.items.map((selector) => (
									<tr key={selector.name} className="hover:bg-muted/50 border-b last:border-0">
										<td className="px-4 py-2.5">
											<div className="flex items-center gap-2">
												<ShieldCheck className="text-muted-foreground size-3.5" />
												<span className="font-mono text-xs font-medium">{selector.name}</span>
											</div>
										</td>
										<td className="text-muted-foreground px-3 py-2.5 text-[11px]">{selector.description || "—"}</td>
										<td className="px-3 py-2.5">
											<code className="bg-muted block max-w-[520px] truncate rounded border px-1.5 py-0.5 font-mono text-[10px]" title={selector.expression}>
												{selector.expression}
											</code>
										</td>
										<td className="px-3 py-2.5 text-right">
											<Button variant="ghost" size="sm" className="h-7 text-xs" onClick={() => setEditing(selector)}>编辑</Button>
											<Button
												variant="ghost" size="icon" className="text-muted-foreground hover:text-destructive size-7"
												aria-label={`删除 ${selector.name}`}
												onClick={async () => {
													try {
														await api.deleteSelector(selector.name);
														toast.success("选择器已删除");
														reload();
													} catch (err) {
														toast.error("删除失败", { description: errorMessage(err) });
													}
												}}
											>
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

			<SelectorTester repositories={repositories.data?.items ?? []} />

			<Dialog open={editing !== null} onOpenChange={(open) => !open && setEditing(null)}>
				<DialogContent className="sm:max-w-lg">
					<DialogHeader>
						<DialogTitle>{editing === "new" ? "新建内容选择器" : "编辑内容选择器"}</DialogTitle>
						<DialogDescription>表达式在保存时校验；修改后对后续协议请求生效。</DialogDescription>
					</DialogHeader>
					{editing ? (
						<SelectorForm
							selector={editing === "new" ? undefined : editing}
							onDone={() => { setEditing(null); reload(); }}
						/>
					) : null}
				</DialogContent>
			</Dialog>
		</AppShell>
	);
}

function SelectorForm({ selector, onDone }: { selector?: ContentSelector; onDone: () => void }) {
	const [name, setName] = useState(selector?.name ?? "");
	const [description, setDescription] = useState(selector?.description ?? "");
	const [expression, setExpression] = useState(selector?.expression ?? 'path.startsWith("public/")');
	const [saving, setSaving] = useState(false);

	async function submit() {
		setSaving(true);
		try {
			if (selector) {
				await api.updateSelector(selector.name, { description, expression });
			} else {
				await api.createSelector({ name: name.trim(), description, expression });
			}
			toast.success("选择器已保存");
			onDone();
		} catch (err) {
			toast.error("保存失败", { description: errorMessage(err) });
		} finally {
			setSaving(false);
		}
	}

	return (
		<div className="space-y-4">
			<div><Label>名称</Label><Input value={name} disabled={!!selector} onChange={(event) => setName(event.target.value)} className="mt-1.5 h-9 font-mono" placeholder="public-only" /></div>
			<div><Label>说明</Label><Input value={description} onChange={(event) => setDescription(event.target.value)} className="mt-1.5 h-9" /></div>
			<div>
				<Label>表达式</Label>
				<textarea
					value={expression}
					onChange={(event) => setExpression(event.target.value)}
					rows={5}
					className="bg-background focus-visible:border-primary mt-1.5 w-full rounded-sm border px-2.5 py-2 font-mono text-[11px] outline-none"
				/>
			</div>
			<DialogFooter>
				<Button isLoading={saving} disabled={!name.trim() && !selector} onClick={() => void submit()}>保存</Button>
			</DialogFooter>
		</div>
	);
}

function SelectorTester({ repositories }: { repositories: Array<{ name: string; format: string }> }) {
	const [expression, setExpression] = useState('path.startsWith("public/")');
	const [format, setFormat] = useState("raw");
	const [repository, setRepository] = useState("raw-hosted");
	const [path, setPath] = useState("public/demo.txt");
	const [result, setResult] = useState<boolean | null>(null);
	const [error, setError] = useState("");
	const [busy, setBusy] = useState(false);

	async function test() {
		setBusy(true);
		setError("");
		try {
			const response = await api.testSelector({ expression, format, repository, path });
			setResult(response.matched);
		} catch (err) {
			setResult(null);
			setError(errorMessage(err));
		} finally {
			setBusy(false);
		}
	}

	return (
		<Card className="mt-3 gap-0 py-0">
			<div className="border-b px-4 py-3">
				<h2 className="text-sm font-semibold">表达式测试</h2>
				<p className="text-muted-foreground mt-0.5 text-[11px]">在保存前验证表达式与示例请求路径。</p>
			</div>
			<div className="grid grid-cols-1 gap-3 p-4 lg:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]">
				<div>
					<Label>表达式</Label>
					<textarea value={expression} onChange={(event) => setExpression(event.target.value)} rows={4} className="bg-background focus-visible:border-primary mt-1.5 w-full rounded-sm border px-2.5 py-2 font-mono text-[11px] outline-none" />
				</div>
				<div className="space-y-3">
					<div>
						<Label>仓库</Label>
						<select value={repository} onChange={(event) => { setRepository(event.target.value); const repo = repositories.find((item) => item.name === event.target.value); if (repo) setFormat(repo.format); }} className="bg-background focus-visible:border-primary mt-1.5 h-9 w-full rounded-sm border px-2.5 text-sm outline-none">
							{repositories.map((repo) => <option key={repo.name} value={repo.name}>{repo.name} · {repo.format}</option>)}
						</select>
					</div>
					<div><Label>格式</Label><Input value={format} onChange={(event) => setFormat(event.target.value)} className="mt-1.5 h-9 font-mono" /></div>
					<div><Label>路径</Label><Input value={path} onChange={(event) => setPath(event.target.value)} className="mt-1.5 h-9 font-mono text-xs" /></div>
				</div>
			</div>
			<div className="flex items-center gap-3 border-t px-4 py-3">
				<Button size="sm" className="h-8" isLoading={busy} onClick={() => void test()}>
					<FlaskConical className="size-3.5" />
					测试匹配
				</Button>
				{result !== null ? <StatusPill tone={result ? "info" : "neutral"} dot>{result ? "匹配" : "不匹配"}</StatusPill> : null}
				{error ? <span className="text-destructive text-xs">{error}</span> : null}
			</div>
		</Card>
	);
}

function Th({ children, className }: { children: React.ReactNode; className?: string }) {
	return <th className={`px-3 py-2 text-left text-[11px] font-medium ${className ?? ""}`}>{children}</th>;
}
