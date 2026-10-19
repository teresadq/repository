export function formatBytes(bytes: number): string {
	if (!Number.isFinite(bytes) || bytes <= 0) return "0 B";
	const units = ["B", "KiB", "MiB", "GiB", "TiB"];
	let value = bytes;
	let index = 0;
	while (value >= 1024 && index < units.length - 1) {
		value /= 1024;
		index += 1;
	}
	const digits = value >= 100 || index === 0 ? 0 : value >= 10 ? 1 : 2;
	return `${value.toFixed(digits)} ${units[index]}`;
}

export function formatNumber(value: number): string {
	return new Intl.NumberFormat("zh-CN").format(value);
}

export function formatDateTime(value?: string): string {
	if (!value) return "—";
	const date = new Date(value);
	if (Number.isNaN(date.getTime()) || date.getFullYear() < 1971) return "—";
	return new Intl.DateTimeFormat("zh-CN", {
		month: "2-digit",
		day: "2-digit",
		hour: "2-digit",
		minute: "2-digit",
		second: "2-digit",
	}).format(date);
}

export function formatDate(value?: string): string {
	if (!value) return "—";
	const date = new Date(value);
	if (Number.isNaN(date.getTime()) || date.getFullYear() < 1971) return "—";
	return new Intl.DateTimeFormat("zh-CN", { month: "2-digit", day: "2-digit" }).format(date);
}

export function relativeTime(value?: string): string {
	if (!value) return "—";
	const date = new Date(value);
	if (Number.isNaN(date.getTime()) || date.getFullYear() < 1971) return "—";
	const seconds = Math.round((Date.now() - date.getTime()) / 1000);
	if (seconds < 45) return "刚刚";
	if (seconds < 3600) return `${Math.max(1, Math.round(seconds / 60))} 分钟前`;
	if (seconds < 86400) return `${Math.round(seconds / 3600)} 小时前`;
	if (seconds < 86400 * 30) return `${Math.round(seconds / 86400)} 天前`;
	return formatDate(value);
}

export function formatUptime(seconds: number): string {
	if (seconds < 60) return `${seconds} 秒`;
	if (seconds < 3600) return `${Math.floor(seconds / 60)} 分钟`;
	if (seconds < 86400) return `${Math.floor(seconds / 3600)} 小时 ${Math.floor((seconds % 3600) / 60)} 分`;
	return `${Math.floor(seconds / 86400)} 天 ${Math.floor((seconds % 86400) / 3600)} 小时`;
}

export function repositoryTypeLabel(type: string): string {
	switch (type) {
		case "hosted":
			return "本地";
		case "proxy":
			return "代理";
		case "group":
			return "分组";
		default:
			return type;
	}
}

export function sourceLabel(source: string): string {
	switch (source) {
		case "proxy":
			return "代理缓存";
		case "hosted":
			return "本地上传";
		default:
			return source || "—";
	}
}

export function kindLabel(kind: string): string {
	switch (kind) {
		case "file":
			return "文件";
		case "chart":
			return "chart";
		case "crate":
			return "crate";
		case "index":
			return "index";
		case "tarball":
			return "tgz";
		case "packument":
			return "json";
		case "distribution":
			return "文件";
		case "manifest":
			return "manifest";
		case "blob":
			return "blob";
		case "lfs-object":
			return "LFS";
		case "source-archive":
			return "源码包";
		case "release":
			return "release";
		case "PACKAGE":
			return "zip";
		case "INFO":
			return "info";
		case "MODULE":
			return "mod";
		case "LIST":
			return "list";
		case "LATEST":
			return "latest";
		default:
			return kind.toLowerCase();
	}
}

export function copyText(value: string): Promise<void> {
	return navigator.clipboard.writeText(value);
}
