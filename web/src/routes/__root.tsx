import { Link, Outlet, createRootRoute } from "@tanstack/react-router";
import { Toaster } from "sonner";

import { Brand } from "@/components/brand";
import { ThemeProvider } from "@/components/themeProvider";
import { Button } from "@/components/ui/button";

export const Route = createRootRoute({
	component: RootComponent,
	notFoundComponent: NotFound,
});

function RootComponent() {
	return (
		<ThemeProvider>
			<Outlet />
			<Toaster position="top-right" richColors closeButton />
		</ThemeProvider>
	);
}

function NotFound() {
	return (
		<div className="bg-background flex min-h-dvh flex-col">
			<header className="px-6 py-5">
				<Brand />
			</header>
			<main className="flex flex-1 flex-col items-center justify-center px-6 pb-20 text-center">
				<p className="text-muted-foreground font-mono text-sm">404</p>
				<h1 className="mt-3 text-2xl font-semibold tracking-tight">页面不存在</h1>
				<p className="text-muted-foreground mt-2 max-w-sm text-sm">
					你访问的页面可能已被移动或删除。可以从概览页继续浏览。
				</p>
				<div className="mt-6 flex items-center gap-2">
					<Button asChild>
						<Link to="/dashboard">返回概览</Link>
					</Button>
					<Button variant="outline" asChild>
						<Link to="/repositories">仓库列表</Link>
					</Button>
				</div>
			</main>
		</div>
	);
}
