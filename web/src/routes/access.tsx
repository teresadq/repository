import { createFileRoute } from "@tanstack/react-router";
import { Copy, KeyRound, Plus, RefreshCw, Search, ShieldCheck, Trash2, Users } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
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
import { Switch } from "@/components/ui/switch";
import {
	api,
	type ApiToken,
	type Role,
	type SecurityGroup,
	type UserAccount,
} from "@/lib/api";
import { copyText, formatDateTime, relativeTime } from "@/lib/format";
import { errorMessage } from "@/lib/repository";
import { useApi } from "@/lib/useApi";

export const Route = createFileRoute("/access")({
	component: AccessPage,
});

const TABS = [
	{ key: "users", label: "用户" },
	{ key: "roles", label: "角色" },
	{ key: "groups", label: "用户组" },
	{ key: "tokens", label: "API Token" },
] as const;

type TabKey = (typeof TABS)[number]["key"];

function AccessPage() {
	const [tab, setTab] = useState<TabKey>("users");
	const [version, setVersion] = useState(0);
	const users = useApi(() => api.users(), [version]);
	const roles = useApi(() => api.roles(), [version]);
	const groups = useApi(() => api.groups(), [version]);
	const tokens = useApi(() => api.tokens(), [version]);
	const anonymous = useApi(() => api.anonymousSettings(), [version]);
	const reload = () => setVersion((value) => value + 1);

	return (
		<AppShell>
			<div className="mb-5 flex flex-wrap items-end justify-between gap-3">
				<div>
					<h1 className="text-xl font-semibold tracking-tight">访问控制</h1>
					<p className="text-muted-foreground mt-1 text-sm">
						用户、角色、用户组与 API Token；权限字符串采用 Nexus 风格{" "}
						<span className="font-mono text-xs">nexus:repository-view:&lt;format&gt;:&lt;repo&gt;:&lt;action&gt;</span>。
					</p>
				</div>
				<Button variant="outline" size="sm" className="h-8" onClick={reload}>
					<RefreshCw className="size-3.5" />
					刷新
				</Button>
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

			{tab === "users" ? (
				<UsersTab
					loading={users.loading}
					users={users.data?.items ?? []}
					roles={roles.data?.items ?? []}
					groups={groups.data?.items ?? []}
					onChanged={reload}
				/>
			) : null}
			{tab === "roles" ? (
				<RolesTab loading={roles.loading} roles={roles.data?.items ?? []} onChanged={reload} />
			) : null}
			{tab === "groups" ? (
				<GroupsTab
					loading={groups.loading}
					groups={groups.data?.items ?? []}
					roles={roles.data?.items ?? []}
					onChanged={reload}
				/>
			) : null}
			{tab === "tokens" ? (
				<TokensTab
					loading={tokens.loading}
					tokens={tokens.data?.items ?? []}
					users={users.data?.items ?? []}
					anonymous={anonymous.data}
					roles={roles.data?.items ?? []}
					onChanged={reload}
				/>
			) : null}
		</AppShell>
	);
}

function UsersTab({
	loading,
	users,
	roles,
	groups,
	onChanged,
}: {
	loading: boolean;
	users: UserAccount[];
	roles: Role[];
	groups: SecurityGroup[];
	onChanged: () => void;
}) {
	const [query, setQuery] = useState("");
	const [editing, setEditing] = useState<UserAccount | "new" | null>(null);
	const [deleteTarget, setDeleteTarget] = useState<UserAccount | null>(null);
	const [busy, setBusy] = useState("");

	const filtered = useMemo(() => {
		const normalized = query.trim().toLowerCase();
		if (!normalized) return users;
		return users.filter((user) => `${user.username} ${user.name} ${user.email}`.toLowerCase().includes(normalized));
	}, [users, query]);

	async function toggle(user: UserAccount, active: boolean) {
		setBusy(user.username);
		try {
			await api.updateUser(user.username, { active });
			toast.success(active ? "用户已启用" : "用户已停用", { description: user.username });
			onChanged();
		} catch (err) {
			toast.error("更新失败", { description: errorMessage(err) });
		} finally {
			setBusy("");
		}
	}

	async function remove() {
		if (!deleteTarget) return;
		setBusy(deleteTarget.username);
		try {
			await api.deleteUser(deleteTarget.username);
			toast.success("用户已删除", { description: deleteTarget.username });
			setDeleteTarget(null);
			onChanged();
		} catch (err) {
			toast.error("删除失败", { description: errorMessage(err) });
		} finally {
			setBusy("");
		}
	}

	return (
		<Card className="gap-0 py-0">
			<Toolbar
				placeholder="搜索用户名、姓名或邮箱…"
				query={query}
				onQuery={setQuery}
				count={`${filtered.length} / ${users.length} 个用户`}
				action={
					<Button size="sm" className="h-8" onClick={() => setEditing("new")}>
						<Plus className="size-3.5" />
						新建用户
					</Button>
				}
			/>
			<div className="overflow-x-auto">
				<table className="w-full text-sm">
					<thead>
						<tr className="bg-muted/50 text-muted-foreground">
							<Th>用户</Th>
							<Th>角色</Th>
							<Th>用户组</Th>
							<Th>状态</Th>
							<Th className="text-right">最近更新</Th>
							<Th className="text-right">操作</Th>
						</tr>
					</thead>
					<tbody>
						{loading ? (
							<SkeletonRows cols={6} />
						) : filtered.length === 0 ? (
							<EmptyRow cols={6} text="没有匹配的用户" />
						) : (
							filtered.map((user) => (
								<tr key={user.username} className="hover:bg-muted/50 border-b last:border-0">
									<td className="px-4 py-2.5">
										<p className="font-mono text-xs font-medium">{user.username}</p>
										<p className="text-muted-foreground text-[11px]">{user.name || "—"}{user.email ? ` · ${user.email}` : ""}</p>
									</td>
									<td className="px-3 py-2.5">
										<div className="flex flex-wrap gap-1">
											{(user.roles ?? []).length === 0 ? <span className="text-muted-foreground text-[11px]">—</span> : null}
											{(user.roles ?? []).map((role) => (
												<StatusPill key={role} tone={role === "nx-admin" ? "solid" : "purple"}>
													{role}
												</StatusPill>
											))}
										</div>
									</td>
									<td className="text-muted-foreground px-3 py-2.5 text-[11px]">{(user.groups ?? []).join(", ") || "—"}</td>
									<td className="px-3 py-2.5">
										<div className="flex items-center gap-2">
											<Switch
												checked={user.active}
												disabled={busy === user.username}
												onCheckedChange={(checked) => void toggle(user, checked)}
												aria-label={`切换 ${user.username} 状态`}
											/>
											<span className="text-muted-foreground text-[11px]">{user.active ? "启用" : "停用"}</span>
										</div>
									</td>
									<td className="text-muted-foreground px-3 py-2.5 text-right font-mono text-[11px]">
										{relativeTime(user.updatedAt)}
									</td>
									<td className="px-3 py-2.5 text-right">
										<div className="flex justify-end gap-1">
											<Button variant="ghost" size="sm" className="h-7 text-xs" onClick={() => setEditing(user)}>
												编辑
											</Button>
											<Button
												variant="ghost"
												size="icon"
												className="text-muted-foreground hover:text-destructive size-7"
												aria-label={`删除 ${user.username}`}
												onClick={() => setDeleteTarget(user)}
											>
												<Trash2 className="size-3.5" />
											</Button>
										</div>
									</td>
								</tr>
							))
						)}
					</tbody>
				</table>
			</div>

			<Dialog open={editing !== null} onOpenChange={(open) => !open && setEditing(null)}>
				<DialogContent className="sm:max-w-lg">
					<DialogHeader>
						<DialogTitle>{editing === "new" ? "新建用户" : "编辑用户"}</DialogTitle>
						<DialogDescription>为用户分配角色和用户组；角色决定仓库读写权限。</DialogDescription>
					</DialogHeader>
					{editing ? (
						<UserForm
							user={editing === "new" ? undefined : editing}
							roles={roles}
							groups={groups}
							onDone={() => {
								setEditing(null);
								onChanged();
							}}
						/>
					) : null}
				</DialogContent>
			</Dialog>

			<Dialog open={!!deleteTarget} onOpenChange={(open) => !open && setDeleteTarget(null)}>
				<DialogContent className="sm:max-w-md">
					<DialogHeader>
						<DialogTitle>删除用户</DialogTitle>
						<DialogDescription>
							确认删除 <span className="text-foreground font-mono">{deleteTarget?.username}</span>？其 API Token 也会一并删除。
						</DialogDescription>
					</DialogHeader>
					<DialogFooter>
						<Button variant="outline" onClick={() => setDeleteTarget(null)}>取消</Button>
						<Button variant="destructive" isLoading={busy === deleteTarget?.username} onClick={() => void remove()}>
							确认删除
						</Button>
					</DialogFooter>
				</DialogContent>
			</Dialog>
		</Card>
	);
}

function UserForm({
	user,
	roles,
	groups,
	onDone,
}: {
	user?: UserAccount;
	roles: Role[];
	groups: SecurityGroup[];
	onDone: () => void;
}) {
	const [username, setUsername] = useState(user?.username ?? "");
	const [name, setName] = useState(user?.name ?? "");
	const [email, setEmail] = useState(user?.email ?? "");
	const [password, setPassword] = useState("");
	const [selectedRoles, setSelectedRoles] = useState<string[]>(user?.roles ?? []);
	const [selectedGroups, setSelectedGroups] = useState<string[]>(user?.groups ?? []);
	const [saving, setSaving] = useState(false);

	async function submit() {
		setSaving(true);
		try {
			if (user) {
				await api.updateUser(user.username, {
					name, email, roles: selectedRoles, groups: selectedGroups,
					...(password ? { password } : {}),
				});
				toast.success("用户已更新", { description: user.username });
			} else {
				await api.createUser({
					username: username.trim(), name, email, password,
					roles: selectedRoles, groups: selectedGroups, active: true,
				});
				toast.success("用户已创建", { description: username });
			}
			onDone();
		} catch (err) {
			toast.error("保存失败", { description: errorMessage(err) });
		} finally {
			setSaving(false);
		}
	}

	return (
		<div className="space-y-4">
			<div className="grid grid-cols-2 gap-3">
				<div>
					<Label>用户名</Label>
					<Input value={username} disabled={!!user} onChange={(event) => setUsername(event.target.value)} className="mt-1.5 h-9 font-mono" />
				</div>
				<div>
					<Label>显示名</Label>
					<Input value={name} onChange={(event) => setName(event.target.value)} className="mt-1.5 h-9" />
				</div>
			</div>
			<div className="grid grid-cols-2 gap-3">
				<div>
					<Label>邮箱</Label>
					<Input value={email} onChange={(event) => setEmail(event.target.value)} className="mt-1.5 h-9" />
				</div>
				<div>
					<Label>{user ? "重置密码（留空不改）" : "密码"}</Label>
					<Input type="password" value={password} onChange={(event) => setPassword(event.target.value)} className="mt-1.5 h-9" />
				</div>
			</div>
			<ChoiceList label="角色" options={roles.map((role) => role.name)} selected={selectedRoles} onChange={setSelectedRoles} />
			<ChoiceList label="用户组" options={groups.map((group) => group.name)} selected={selectedGroups} onChange={setSelectedGroups} />
			<DialogFooter>
				<Button isLoading={saving} disabled={!username.trim() || (!user && password.length < 6)} onClick={() => void submit()}>
					保存
				</Button>
			</DialogFooter>
		</div>
	);
}

function RolesTab({ loading, roles, onChanged }: { loading: boolean; roles: Role[]; onChanged: () => void }) {
	const [editing, setEditing] = useState<Role | "new" | null>(null);
	const [deleteTarget, setDeleteTarget] = useState<Role | null>(null);

	return (
		<Card className="gap-0 py-0">
			<Toolbar
				placeholder="角色由名称和权限字符串组成"
				query=""
				onQuery={() => undefined}
				count={`${roles.length} 个角色`}
				hideSearch
				action={
					<Button size="sm" className="h-8" onClick={() => setEditing("new")}>
						<Plus className="size-3.5" />
						新建角色
					</Button>
				}
			/>
			<div className="overflow-x-auto">
				<table className="w-full text-sm">
					<thead>
						<tr className="bg-muted/50 text-muted-foreground">
							<Th>角色</Th>
							<Th>说明</Th>
							<Th>权限</Th>
							<Th className="text-right">操作</Th>
						</tr>
					</thead>
					<tbody>
						{loading ? (
							<SkeletonRows cols={4} />
						) : (
							roles.map((role) => (
								<tr key={role.name} className="hover:bg-muted/50 border-b last:border-0">
									<td className="px-4 py-2.5">
										<div className="flex items-center gap-2">
											<ShieldCheck className="text-muted-foreground size-3.5" />
											<span className="font-mono text-xs font-medium">{role.name}</span>
											{role.builtIn ? <StatusPill tone="neutral">内置</StatusPill> : null}
										</div>
									</td>
									<td className="text-muted-foreground px-3 py-2.5 text-[11px]">{role.description || "—"}</td>
									<td className="px-3 py-2.5">
										<div className="flex max-w-[520px] flex-wrap gap-1">
											{(role.privileges ?? []).slice(0, 3).map((privilege) => (
												<code key={privilege} className="bg-muted rounded border px-1.5 py-0.5 font-mono text-[10px]">{privilege}</code>
											))}
											{(role.privileges ?? []).length > 3 ? (
												<span className="text-muted-foreground text-[10px]">+{(role.privileges ?? []).length - 3}</span>
											) : null}
										</div>
									</td>
									<td className="px-3 py-2.5 text-right">
										<Button variant="ghost" size="sm" className="h-7 text-xs" disabled={role.name === "nx-admin"} onClick={() => setEditing(role)}>
											编辑
										</Button>
										{!role.builtIn ? (
											<Button
												variant="ghost"
												size="icon"
												className="text-muted-foreground hover:text-destructive size-7"
												aria-label={`删除 ${role.name}`}
												onClick={() => setDeleteTarget(role)}
											>
												<Trash2 className="size-3.5" />
											</Button>
										) : null}
									</td>
								</tr>
							))
						)}
					</tbody>
				</table>
			</div>

			<Dialog open={editing !== null} onOpenChange={(open) => !open && setEditing(null)}>
				<DialogContent className="sm:max-w-lg">
					<DialogHeader>
						<DialogTitle>{editing === "new" ? "新建角色" : "编辑角色"}</DialogTitle>
						<DialogDescription>每行一个权限字符串；* 匹配任意值。</DialogDescription>
					</DialogHeader>
					{editing ? (
						<RoleForm
							role={editing === "new" ? undefined : editing}
							onDone={() => {
								setEditing(null);
								onChanged();
							}}
						/>
					) : null}
				</DialogContent>
			</Dialog>

			<ConfirmDelete
				open={!!deleteTarget}
				title="删除角色"
				description={`确认删除角色 ${deleteTarget?.name}？`}
				onClose={() => setDeleteTarget(null)}
				onConfirm={async () => {
					if (!deleteTarget) return;
					try {
						await api.deleteRole(deleteTarget.name);
						toast.success("角色已删除");
						setDeleteTarget(null);
						onChanged();
					} catch (err) {
						toast.error("删除失败", { description: errorMessage(err) });
					}
				}}
			/>
		</Card>
	);
}

function RoleForm({ role, onDone }: { role?: Role; onDone: () => void }) {
	const [name, setName] = useState(role?.name ?? "");
	const [description, setDescription] = useState(role?.description ?? "");
	const [privileges, setPrivileges] = useState((role?.privileges ?? []).join("\n"));
	const [saving, setSaving] = useState(false);

	async function submit() {
		const list = privileges.split("\n").map((line) => line.trim()).filter(Boolean);
		setSaving(true);
		try {
			if (role) {
				await api.updateRole(role.name, { description, privileges: list });
			} else {
				await api.createRole({ name: name.trim(), description, privileges: list });
			}
			toast.success("角色已保存");
			onDone();
		} catch (err) {
			toast.error("保存失败", { description: errorMessage(err) });
		} finally {
			setSaving(false);
		}
	}

	return (
		<div className="space-y-4">
			<div>
				<Label>名称</Label>
				<Input value={name} disabled={!!role} onChange={(event) => setName(event.target.value)} className="mt-1.5 h-9 font-mono" />
			</div>
			<div>
				<Label>说明</Label>
				<Input value={description} onChange={(event) => setDescription(event.target.value)} className="mt-1.5 h-9" />
			</div>
			<div>
				<Label>权限（每行一个）</Label>
				<textarea
					value={privileges}
					onChange={(event) => setPrivileges(event.target.value)}
					rows={7}
					className="bg-background focus-visible:border-primary mt-1.5 w-full rounded-sm border px-2.5 py-2 font-mono text-[11px] outline-none"
					placeholder={"nexus:repository-view:*:*:read\nnexus:repository-view:*:*:write"}
				/>
				<p className="text-muted-foreground mt-1 text-[11px]">
					示例：<span className="font-mono">nexus:repository-view:maven:*:read</span>、{" "}
					<span className="font-mono">nexus:repository-admin:*:*:*</span>、<span className="font-mono">*</span>
				</p>
			</div>
			<DialogFooter>
				<Button isLoading={saving} disabled={!name.trim() && !role} onClick={() => void submit()}>保存</Button>
			</DialogFooter>
		</div>
	);
}

function GroupsTab({
	loading,
	groups,
	roles,
	onChanged,
}: {
	loading: boolean;
	groups: SecurityGroup[];
	roles: Role[];
	onChanged: () => void;
}) {
	const [editing, setEditing] = useState<SecurityGroup | "new" | null>(null);
	const [deleteTarget, setDeleteTarget] = useState<SecurityGroup | null>(null);

	return (
		<Card className="gap-0 py-0">
			<Toolbar
				placeholder=""
				query=""
				onQuery={() => undefined}
				count={`${groups.length} 个用户组`}
				hideSearch
				action={
					<Button size="sm" className="h-8" onClick={() => setEditing("new")}>
						<Plus className="size-3.5" />
						新建用户组
					</Button>
				}
			/>
			<div className="overflow-x-auto">
				<table className="w-full text-sm">
					<thead>
						<tr className="bg-muted/50 text-muted-foreground">
							<Th>用户组</Th>
							<Th>说明</Th>
							<Th>角色</Th>
							<Th className="text-right">操作</Th>
						</tr>
					</thead>
					<tbody>
						{loading ? (
							<SkeletonRows cols={4} />
						) : groups.length === 0 ? (
							<EmptyRow cols={4} text="暂无用户组" />
						) : (
							groups.map((group) => (
								<tr key={group.name} className="hover:bg-muted/50 border-b last:border-0">
									<td className="px-4 py-2.5 font-mono text-xs font-medium">{group.name}</td>
									<td className="text-muted-foreground px-3 py-2.5 text-[11px]">{group.description || "—"}</td>
									<td className="px-3 py-2.5">
										<div className="flex flex-wrap gap-1">
											{(group.roles ?? []).map((role) => (
												<StatusPill key={role} tone="purple">{role}</StatusPill>
											))}
										</div>
									</td>
									<td className="px-3 py-2.5 text-right">
										<Button variant="ghost" size="sm" className="h-7 text-xs" onClick={() => setEditing(group)}>编辑</Button>
										<Button
											variant="ghost"
											size="icon"
											className="text-muted-foreground hover:text-destructive size-7"
											aria-label={`删除 ${group.name}`}
											onClick={() => setDeleteTarget(group)}
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

			<Dialog open={editing !== null} onOpenChange={(open) => !open && setEditing(null)}>
				<DialogContent className="sm:max-w-md">
					<DialogHeader>
						<DialogTitle>{editing === "new" ? "新建用户组" : "编辑用户组"}</DialogTitle>
						<DialogDescription>用户组把一组角色打包分配给用户。</DialogDescription>
					</DialogHeader>
					{editing ? (
						<GroupForm
							group={editing === "new" ? undefined : editing}
							roles={roles}
							onDone={() => {
								setEditing(null);
								onChanged();
							}}
						/>
					) : null}
				</DialogContent>
			</Dialog>

			<ConfirmDelete
				open={!!deleteTarget}
				title="删除用户组"
				description={`确认删除用户组 ${deleteTarget?.name}？`}
				onClose={() => setDeleteTarget(null)}
				onConfirm={async () => {
					if (!deleteTarget) return;
					try {
						await api.deleteGroup(deleteTarget.name);
						toast.success("用户组已删除");
						setDeleteTarget(null);
						onChanged();
					} catch (err) {
						toast.error("删除失败", { description: errorMessage(err) });
					}
				}}
			/>
		</Card>
	);
}

function GroupForm({ group, roles, onDone }: { group?: SecurityGroup; roles: Role[]; onDone: () => void }) {
	const [name, setName] = useState(group?.name ?? "");
	const [description, setDescription] = useState(group?.description ?? "");
	const [selected, setSelected] = useState<string[]>(group?.roles ?? []);
	const [saving, setSaving] = useState(false);

	async function submit() {
		setSaving(true);
		try {
			if (group) {
				await api.updateGroup(group.name, { description, roles: selected });
			} else {
				await api.createGroup({ name: name.trim(), description, roles: selected });
			}
			toast.success("用户组已保存");
			onDone();
		} catch (err) {
			toast.error("保存失败", { description: errorMessage(err) });
		} finally {
			setSaving(false);
		}
	}

	return (
		<div className="space-y-4">
			<div>
				<Label>名称</Label>
				<Input value={name} disabled={!!group} onChange={(event) => setName(event.target.value)} className="mt-1.5 h-9 font-mono" />
			</div>
			<div>
				<Label>说明</Label>
				<Input value={description} onChange={(event) => setDescription(event.target.value)} className="mt-1.5 h-9" />
			</div>
			<ChoiceList label="角色" options={roles.map((role) => role.name)} selected={selected} onChange={setSelected} />
			<DialogFooter>
				<Button isLoading={saving} disabled={!name.trim() && !group} onClick={() => void submit()}>保存</Button>
			</DialogFooter>
		</div>
	);
}

function TokensTab({
	loading,
	tokens,
	users,
	anonymous,
	roles,
	onChanged,
}: {
	loading: boolean;
	tokens: ApiToken[];
	users: UserAccount[];
	anonymous?: { enabled: boolean; roles: string[] };
	roles: Role[];
	onChanged: () => void;
}) {
	const [createOpen, setCreateOpen] = useState(false);
	const [newToken, setNewToken] = useState<ApiToken | null>(null);
	const [anonymousEnabled, setAnonymousEnabled] = useState(anonymous?.enabled ?? true);
	const [anonymousRoles, setAnonymousRoles] = useState<string[]>(anonymous?.roles ?? []);
	const [savingAnonymous, setSavingAnonymous] = useState(false);

	useEffect(() => {
		if (!anonymous) return;
		setAnonymousEnabled(anonymous.enabled);
		setAnonymousRoles(anonymous.roles ?? []);
	}, [anonymous]);

	async function saveAnonymous() {
		setSavingAnonymous(true);
		try {
			await api.updateAnonymousSettings({ enabled: anonymousEnabled, roles: anonymousRoles });
			toast.success("匿名访问设置已保存");
			onChanged();
		} catch (err) {
			toast.error("保存失败", { description: errorMessage(err) });
		} finally {
			setSavingAnonymous(false);
		}
	}

	return (
		<div className="space-y-3">
			<Card className="gap-0 py-0">
				<div className="flex items-center justify-between border-b px-4 py-3">
					<div>
						<h2 className="text-sm font-semibold">匿名访问</h2>
						<p className="text-muted-foreground mt-0.5 text-[11px]">未登录客户端读取仓库时使用的角色。</p>
					</div>
					<Switch checked={anonymousEnabled} onCheckedChange={setAnonymousEnabled} aria-label="匿名访问" />
				</div>
				<div className="flex flex-wrap items-center gap-2 p-4">
					{roles.map((role) => {
						const checked = anonymousRoles.includes(role.name);
						return (
							<label key={role.name} className="hover:bg-muted/60 flex cursor-pointer items-center gap-2 rounded-sm border px-2.5 py-1.5 text-xs">
								<input
									type="checkbox"
									checked={checked}
									onChange={() =>
										setAnonymousRoles((current) =>
											checked ? current.filter((item) => item !== role.name) : [...current, role.name],
										)
									}
								/>
								<span className="font-mono">{role.name}</span>
							</label>
						);
					})}
					<Button size="sm" className="ml-auto h-8" isLoading={savingAnonymous} onClick={() => void saveAnonymous()}>
						保存
					</Button>
				</div>
			</Card>

			<Card className="gap-0 py-0">
				<Toolbar
					placeholder=""
					query=""
					onQuery={() => undefined}
					count={`${tokens.length} 个 Token`}
					hideSearch
					action={
						<Button size="sm" className="h-8" onClick={() => setCreateOpen(true)}>
							<KeyRound className="size-3.5" />
							创建 Token
						</Button>
					}
				/>
				<div className="overflow-x-auto">
					<table className="w-full text-sm">
						<thead>
							<tr className="bg-muted/50 text-muted-foreground">
								<Th>名称</Th>
								<Th>用户</Th>
								<Th>前缀</Th>
								<Th>过期时间</Th>
								<Th className="text-right">最近使用</Th>
								<Th className="text-right">操作</Th>
							</tr>
						</thead>
						<tbody>
							{loading ? (
								<SkeletonRows cols={6} />
							) : tokens.length === 0 ? (
								<EmptyRow cols={6} text="暂无 Token" />
							) : (
								tokens.map((token) => (
									<tr key={token.id} className="hover:bg-muted/50 border-b last:border-0">
										<td className="px-4 py-2.5 text-xs font-medium">{token.name}</td>
										<td className="px-3 py-2.5 font-mono text-xs">{token.username}</td>
										<td className="px-3 py-2.5 font-mono text-[11px]">{token.prefix}…</td>
										<td className="text-muted-foreground px-3 py-2.5 text-[11px]">{token.expiresAt ? formatDateTime(token.expiresAt) : "永不过期"}</td>
										<td className="text-muted-foreground px-3 py-2.5 text-right text-[11px]">{token.lastUsedAt ? relativeTime(token.lastUsedAt) : "从未"}</td>
										<td className="px-3 py-2.5 text-right">
											<Button
												variant="ghost"
												size="icon"
												className="text-muted-foreground hover:text-destructive size-7"
												aria-label={`删除 ${token.name}`}
												onClick={async () => {
													try {
														await api.deleteToken(token.id);
														toast.success("Token 已删除");
														onChanged();
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

			<Dialog open={createOpen} onOpenChange={setCreateOpen}>
				<DialogContent className="sm:max-w-md">
					<DialogHeader>
						<DialogTitle>创建 API Token</DialogTitle>
						<DialogDescription>Token 只显示一次；用于 npm/pip/docker/curl 的 Basic 或 Bearer 认证。</DialogDescription>
					</DialogHeader>
					<TokenForm
						users={users}
						onCreated={(token) => {
							setCreateOpen(false);
							setNewToken(token);
							onChanged();
						}}
					/>
				</DialogContent>
			</Dialog>

			<Dialog open={!!newToken} onOpenChange={(open) => !open && setNewToken(null)}>
				<DialogContent className="sm:max-w-md">
					<DialogHeader>
						<DialogTitle>Token 已创建</DialogTitle>
						<DialogDescription>请立即复制保存，关闭后无法再次查看。</DialogDescription>
					</DialogHeader>
					<div className="flex items-center gap-2">
						<code className="bg-muted flex-1 overflow-x-auto rounded-sm border px-2.5 py-2 font-mono text-[11px]">{newToken?.token}</code>
						<Button
							variant="outline"
							size="icon"
							className="size-8"
							aria-label="复制 Token"
							onClick={() => void copyText(newToken?.token ?? "").then(() => toast.success("Token 已复制"))}
						>
							<Copy className="size-3.5" />
						</Button>
					</div>
					<DialogFooter>
						<Button onClick={() => setNewToken(null)}>完成</Button>
					</DialogFooter>
				</DialogContent>
			</Dialog>
		</div>
	);
}

function TokenForm({ users, onCreated }: { users: UserAccount[]; onCreated: (token: ApiToken) => void }) {
	const [name, setName] = useState("");
	const [username, setUsername] = useState(users[0]?.username ?? "admin");
	const [days, setDays] = useState("90");
	const [saving, setSaving] = useState(false);

	async function submit() {
		setSaving(true);
		try {
			const expires = Number(days) > 0 ? new Date(Date.now() + Number(days) * 86400000).toISOString() : undefined;
			const token = await api.createToken({ name: name.trim(), username, expiresAt: expires });
			onCreated(token);
		} catch (err) {
			toast.error("创建失败", { description: errorMessage(err) });
		} finally {
			setSaving(false);
		}
	}

	return (
		<div className="space-y-4">
			<div>
				<Label>名称</Label>
				<Input value={name} onChange={(event) => setName(event.target.value)} className="mt-1.5 h-9" placeholder="ci-publish" />
			</div>
			<div>
				<Label>用户</Label>
				<select
					value={username}
					onChange={(event) => setUsername(event.target.value)}
					className="bg-background focus-visible:border-primary mt-1.5 h-9 w-full rounded-sm border px-2.5 text-sm outline-none"
				>
					{users.map((user) => <option key={user.username} value={user.username}>{user.username}</option>)}
				</select>
			</div>
			<div>
				<Label>有效期（天，0 为永不过期）</Label>
				<Input value={days} onChange={(event) => setDays(event.target.value)} className="mt-1.5 h-9 font-mono" />
			</div>
			<DialogFooter>
				<Button isLoading={saving} disabled={!name.trim() || !username} onClick={() => void submit()}>创建</Button>
			</DialogFooter>
		</div>
	);
}

function Toolbar({
	placeholder,
	query,
	onQuery,
	count,
	action,
	hideSearch,
}: {
	placeholder: string;
	query: string;
	onQuery: (value: string) => void;
	count: string;
	action: React.ReactNode;
	hideSearch?: boolean;
}) {
	return (
		<div className="flex flex-wrap items-center gap-2 border-b px-3 py-2.5">
			{hideSearch ? null : (
				<div className="relative">
					<Search className="text-muted-foreground pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2" />
					<Input value={query} onChange={(event) => onQuery(event.target.value)} placeholder={placeholder} className="h-8 w-64 pl-8 text-xs" />
				</div>
			)}
			<span className="text-muted-foreground ml-auto text-[11px]">{count}</span>
			{action}
		</div>
	);
}

function ChoiceList({
	label,
	options,
	selected,
	onChange,
}: {
	label: string;
	options: string[];
	selected: string[];
	onChange: (values: string[]) => void;
}) {
	return (
		<div>
			<Label>{label}</Label>
			<div className="mt-1.5 flex flex-wrap gap-2">
				{options.length === 0 ? <span className="text-muted-foreground text-xs">暂无可选项</span> : null}
				{options.map((option) => {
					const checked = selected.includes(option);
					return (
						<label key={option} className="hover:bg-muted/60 flex cursor-pointer items-center gap-2 rounded-sm border px-2.5 py-1.5 text-xs">
							<input
								type="checkbox"
								checked={checked}
								onChange={() => onChange(checked ? selected.filter((item) => item !== option) : [...selected, option])}
							/>
							<span className="font-mono">{option}</span>
						</label>
					);
				})}
			</div>
		</div>
	);
}

function ConfirmDelete({
	open,
	title,
	description,
	onClose,
	onConfirm,
}: {
	open: boolean;
	title: string;
	description: string;
	onClose: () => void;
	onConfirm: () => Promise<void>;
}) {
	const [busy, setBusy] = useState(false);
	return (
		<Dialog open={open} onOpenChange={(next) => !next && onClose()}>
			<DialogContent className="sm:max-w-md">
				<DialogHeader>
					<DialogTitle>{title}</DialogTitle>
					<DialogDescription>{description}</DialogDescription>
				</DialogHeader>
				<DialogFooter>
					<Button variant="outline" onClick={onClose}>取消</Button>
					<Button
						variant="destructive"
						isLoading={busy}
						onClick={() => {
							setBusy(true);
							void onConfirm().finally(() => setBusy(false));
						}}
					>
						确认删除
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}

function Th({ children, className }: { children: React.ReactNode; className?: string }) {
	return <th className={`px-3 py-2 text-left text-[11px] font-medium ${className ?? ""}`}>{children}</th>;
}

function SkeletonRows({ cols }: { cols: number }) {
	return (
		<>
			{Array.from({ length: 4 }).map((_, index) => (
				<tr key={index} className="border-b last:border-0">
					<td className="px-4 py-3" colSpan={cols}>
						<Skeleton className="h-5 w-full" />
					</td>
				</tr>
			))}
		</>
	);
}

function EmptyRow({ cols, text }: { cols: number; text: string }) {
	return (
		<tr>
			<td className="text-muted-foreground px-4 py-10 text-center text-sm" colSpan={cols}>
				<Users className="mx-auto mb-2 size-5 opacity-50" />
				{text}
			</td>
		</tr>
	);
}
