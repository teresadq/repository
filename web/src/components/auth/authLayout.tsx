import type { ReactNode } from "react";

import { Brand } from "@/components/brand";
import { ThemeToggle } from "@/components/themeToggle";

/**
 * 认证页布局：中性灰色背景 + 居中白色卡片，与 Bifrost 控制台的
 * 「灰底、白卡、细边框」视觉语言一致，不引入额外品牌色。
 */
export function AuthLayout({ children }: { children: ReactNode }) {
	return (
		<div className="flex min-h-dvh flex-col bg-background">
			<header className="flex items-center justify-between px-6 py-5">
				<Brand />
				<ThemeToggle />
			</header>

			<main className="flex flex-1 items-start justify-center px-5 pt-6 pb-16">
				<div className="animate-rise bg-card w-full max-w-[420px] rounded-xl border p-8 shadow-sm">{children}</div>
			</main>

			<footer className="px-6 pb-6 text-center text-[11px] text-muted-foreground">
				kkrepo-go · Go module proxy console · 设计参考 Bifrost UI 设计系统
			</footer>
		</div>
	);
}
