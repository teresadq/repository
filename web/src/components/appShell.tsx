import { Link, useLocation, useNavigate } from "@tanstack/react-router";
import { ChevronRight, LogOut, PanelLeft, Search } from "lucide-react";
import { useEffect, useState, type ReactNode } from "react";
import { toast } from "sonner";

import { Brand } from "@/components/brand";
import { CommandPalette } from "@/components/commandPalette";
import { ThemeToggle } from "@/components/themeToggle";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuLabel,
	DropdownMenuSeparator,
	DropdownMenuTrigger,
} from "@/components/ui/dropdownMenu";
import { Separator } from "@/components/ui/separator";
import { api, type SessionResponse } from "@/lib/api";
import { NAV_GROUPS, pageTitle } from "@/lib/navigation";
import { cn } from "@/lib/utils";

type SessionUser = NonNullable<SessionResponse["user"]>;

function Sidebar({ onSearch }: { onSearch: () => void }) {
	const location = useLocation();

	return (
		<aside className="bg-sidebar fixed inset-y-0 left-0 z-20 hidden w-60 flex-col border-r md:flex">
			<div className="flex h-14 items-center justify-between px-4">
				<Brand />
				<Button
					variant="ghost"
					size="icon"
					className="text-muted-foreground size-7"
					aria-label="收起侧边栏"
					onClick={() => toast.info("kkrepo-go 控制台暂不支持折叠侧边栏")}
				>
					<PanelLeft className="size-4" />
				</Button>
			</div>

			<div className="px-3 pb-2">
				<button
					type="button"
					onClick={onSearch}
					className="bg-card text-muted-foreground hover:border-ring/60 flex h-8 w-full cursor-pointer items-center gap-2 rounded-md border px-2.5 transition-colors"
				>
					<Search className="size-3.5 shrink-0" />
					<span className="flex-1 text-left text-xs">搜索…</span>
					<kbd className="text-muted-foreground/70 bg-muted rounded border px-1 font-mono text-[10px]">⌘K</kbd>
				</button>
			</div>

			<nav className="custom-scrollbar flex-1 px-3 py-2">
				{NAV_GROUPS.map((group) => (
					<div key={group.label} className="mb-5">
						<p className="text-muted-foreground/60 mb-1 px-2 text-[10px] font-medium tracking-[0.08em] uppercase">
							{group.label}
						</p>
						<ul className="space-y-0.5">
							{group.items.map((item) => {
								const Icon = item.icon;
								const active = location.pathname === item.to || location.pathname.startsWith(item.to + "/");
								return (
									<li key={item.to}>
										<Link
											to={item.to}
											aria-current={active ? "page" : undefined}
											className={cn(
												"flex items-center gap-2.5 rounded-md px-2 py-[7px] text-[13px] transition-colors",
												active
													? "bg-sidebar-accent text-foreground font-medium"
													: "text-muted-foreground hover:bg-sidebar-accent hover:text-foreground",
											)}
										>
											<Icon className={cn("size-4 shrink-0", active ? "text-foreground" : "text-muted-foreground/80")} />
											{item.label}
										</Link>
									</li>
								);
							})}
						</ul>
					</div>
				))}
			</nav>

			<div className="px-3">
				<p className="text-muted-foreground/50 text-center font-mono text-[10px]">v0.2.0 · artifact repository</p>
			</div>

			<SidebarUser />
		</aside>
	);
}

function SidebarUser() {
	const navigate = useNavigate();
	const [user, setUser] = useState<SessionUser | null>(null);

	useEffect(() => {
		api
			.session()
			.then((session) => {
				if (!session.authenticated) {
					void navigate({ to: "/login", replace: true });
					return;
				}
				setUser(session.user);
			})
			.catch(() => void navigate({ to: "/login", replace: true }));
	}, [navigate]);

	async function logout() {
		try {
			await api.logout();
		} finally {
			toast.success("已退出登录");
			void navigate({ to: "/login", replace: true });
		}
	}

	const initial = user?.username?.slice(0, 1).toUpperCase() ?? "A";
	return (
		<div className="border-t p-3">
			<div className="flex items-center gap-2.5 rounded-md px-1.5 py-1.5">
				<Avatar className="size-7">
					<AvatarFallback className="bg-secondary text-[10px] font-medium">{initial}</AvatarFallback>
				</Avatar>
				<div className="min-w-0 flex-1">
					<p className="truncate text-xs font-medium">{user?.name ?? "管理员"}</p>
					<p className="text-muted-foreground truncate text-[11px]">{user?.username ?? "admin"}</p>
				</div>
				<Button
					variant="ghost"
					size="icon"
					className="text-muted-foreground size-7"
					aria-label="退出登录"
					onClick={() => void logout()}
				>
					<LogOut className="size-3.5" />
				</Button>
			</div>
		</div>
	);
}

function Topbar({ onSearch }: { onSearch: () => void }) {
	const location = useLocation();
	const title = pageTitle(location.pathname);

	return (
		<header className="bg-card sticky top-0 z-10 flex h-14 items-center gap-3 border-b px-4 md:px-6">
			<Link to="/dashboard" className="md:hidden">
				<Brand markClassName="size-7" />
			</Link>

			<nav className="text-muted-foreground hidden items-center gap-1.5 text-[13px] md:flex" aria-label="面包屑">
				<span>kkrepo-go</span>
				<ChevronRight className="size-3.5 opacity-50" />
				<span className="text-foreground font-medium">{title}</span>
			</nav>

			<div className="ml-auto flex items-center gap-1">
				<button
					type="button"
					onClick={onSearch}
					className="bg-background text-muted-foreground hover:border-ring/60 mr-2 hidden h-8 w-64 cursor-pointer items-center gap-2 rounded-md border px-2.5 transition-colors sm:flex"
				>
					<Search className="size-3.5 shrink-0" />
					<span className="flex-1 text-left text-[13px]">搜索页面或操作…</span>
					<kbd className="text-muted-foreground/70 rounded border px-1.5 py-0.5 font-mono text-[10px]">⌘K</kbd>
				</button>

				<Button
					variant="ghost"
					size="icon"
					className="text-muted-foreground sm:hidden"
					aria-label="搜索页面"
					onClick={onSearch}
				>
					<Search className="size-4" />
				</Button>

				<ThemeToggle />

				<Separator orientation="vertical" className="mx-1.5 h-5" />

				<Button variant="ghost" size="sm" className="text-muted-foreground h-8" asChild>
					<Link to="/status">系统状态</Link>
				</Button>

				<DropdownMenu>
					<DropdownMenuTrigger asChild>
						<button
							type="button"
							aria-label="账户菜单"
							className="hover:bg-accent flex cursor-pointer items-center gap-1.5 rounded-md px-1 py-1 transition-colors"
						>
							<Avatar className="size-7">
								<AvatarFallback className="bg-secondary text-[10px] font-medium">A</AvatarFallback>
							</Avatar>
							<ChevronRight className="text-muted-foreground hidden size-3.5 rotate-90 sm:inline" />
						</button>
					</DropdownMenuTrigger>
					<DropdownMenuContent align="end" className="w-48">
						<DropdownMenuLabel>
							<p className="text-sm font-medium">管理员</p>
							<p className="text-muted-foreground text-xs font-normal">artifact console</p>
						</DropdownMenuLabel>
						<DropdownMenuSeparator />
						<DropdownMenuItem asChild>
							<Link to="/settings">系统设置</Link>
						</DropdownMenuItem>
						<DropdownMenuItem asChild>
							<Link to="/status">系统状态</Link>
						</DropdownMenuItem>
						<DropdownMenuSeparator />
						<DropdownMenuItem
							variant="destructive"
							onClick={() => {
								void api.logout().finally(() => {
									toast.success("已退出登录");
									window.location.assign("/login");
								});
							}}
						>
							<LogOut className="size-4" />
							退出登录
						</DropdownMenuItem>
					</DropdownMenuContent>
				</DropdownMenu>
			</div>
		</header>
	);
}

export function AppShell({ children }: { children: ReactNode }) {
	const [paletteOpen, setPaletteOpen] = useState(false);

	return (
		<div className="bg-card min-h-dvh">
			<Sidebar onSearch={() => setPaletteOpen(true)} />
			<div className="md:pl-60">
				<Topbar onSearch={() => setPaletteOpen(true)} />
				<main className="mx-auto w-full max-w-[1400px] px-4 py-6 md:px-8">{children}</main>
			</div>
			<CommandPalette open={paletteOpen} onOpenChange={setPaletteOpen} />
		</div>
	);
}
