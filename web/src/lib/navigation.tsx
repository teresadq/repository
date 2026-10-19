import {
	Activity,
	Boxes,
	ClipboardList,
	Filter,
	LayoutDashboard,
	type LucideIcon,
	Search,
	ScrollText,
	Settings,
	ShieldCheck,
	Trash2,
} from "lucide-react";

export type AppPath =
	| "/dashboard"
	| "/repositories"
	| "/search"
	| "/access"
	| "/selectors"
	| "/audit"
	| "/logs"
	| "/cleanup"
	| "/status"
	| "/settings";

export type NavItem = {
	label: string;
	to: AppPath;
	icon: LucideIcon;
	badge?: string;
	keywords?: string;
};

export const NAV_GROUPS: { label: string; items: NavItem[] }[] = [
	{
		label: "工作台",
		items: [
			{ label: "概览", to: "/dashboard", icon: LayoutDashboard, keywords: "dashboard overview home 概览" },
			{ label: "全局搜索", to: "/search", icon: Search, keywords: "search checksum path 搜索" },
		],
	},
	{
		label: "仓库",
		items: [{ label: "仓库列表", to: "/repositories", icon: Boxes, keywords: "repositories go modules 仓库" }],
	},
	{
		label: "访问与安全",
		items: [
			{ label: "访问控制", to: "/access", icon: ShieldCheck, keywords: "users roles groups tokens 用户 角色 权限" },
			{ label: "内容选择器", to: "/selectors", icon: Filter, keywords: "content selector path 内容选择器" },
			{ label: "审计日志", to: "/audit", icon: ClipboardList, keywords: "audit security 审计" },
		],
	},
	{
		label: "系统",
		items: [
			{ label: "请求日志", to: "/logs", icon: ScrollText, keywords: "logs requests 日志" },
			{ label: "系统状态", to: "/status", icon: Activity, keywords: "status health uptime 状态" },
			{ label: "清理策略", to: "/cleanup", icon: Trash2, keywords: "cleanup retention 清理" },
			{ label: "系统设置", to: "/settings", icon: Settings, keywords: "settings goproxy 设置" },
		],
	},
];

export const ALL_PAGES = NAV_GROUPS.flatMap((group) => group.items);

export const EXTRA_PAGES: NavItem[] = [];

export function pageTitle(pathname: string): string {
	if (pathname.startsWith("/repositories/")) return "仓库详情";
	const match = ALL_PAGES.find((item) => item.to === pathname);
	return match?.label ?? "工作台";
}
