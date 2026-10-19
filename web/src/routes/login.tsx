import { zodResolver } from "@hookform/resolvers/zod";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { Eye, EyeOff, KeyRound } from "lucide-react";
import { useEffect, useState } from "react";
import { useForm } from "react-hook-form";
import { toast } from "sonner";
import { z } from "zod";

import { AuthLayout } from "@/components/auth/authLayout";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { api } from "@/lib/api";

export const Route = createFileRoute("/login")({
	component: LoginPage,
});

const loginSchema = z.object({
	username: z.string().min(1, "请输入用户名"),
	password: z.string().min(1, "请输入密码"),
});

type LoginValues = z.infer<typeof loginSchema>;

function FieldError({ message }: { message?: string }) {
	if (!message) return null;
	return <p className="text-destructive mt-1.5 text-xs">{message}</p>;
}

function LoginPage() {
	const navigate = useNavigate();
	const [showPassword, setShowPassword] = useState(false);
	const [submitting, setSubmitting] = useState(false);

	const form = useForm<LoginValues>({
		resolver: zodResolver(loginSchema),
		defaultValues: { username: "admin", password: "" },
		mode: "onTouched",
	});

	const [oidcEnabled, setOidcEnabled] = useState(false);

	useEffect(() => {
		api
			.session()
			.then((session) => {
				if (session.authenticated) {
					void navigate({ to: "/dashboard", replace: true });
				}
			})
			.catch(() => undefined);
		api
			.authOptions()
			.then((options) => setOidcEnabled(options.oidc))
			.catch(() => undefined);
	}, [navigate]);

	async function onSubmit(values: LoginValues) {
		setSubmitting(true);
		try {
			await api.login(values.username, values.password);
			toast.success("登录成功", { description: "正在进入控制台…" });
			void navigate({ to: "/dashboard" });
		} catch (error) {
			toast.error("登录失败", {
				description: error instanceof Error ? error.message : "请检查用户名和密码",
			});
		} finally {
			setSubmitting(false);
		}
	}

	return (
		<AuthLayout>
			<h1 className="text-2xl font-semibold tracking-tight">欢迎回来</h1>
			<p className="text-muted-foreground mt-2 text-sm">登录后管理制品仓库、用户权限、缓存与系统状态。</p>

			<form className="mt-7 space-y-4" onSubmit={form.handleSubmit(onSubmit)} noValidate>
				<div>
					<Label htmlFor="username">用户名</Label>
					<Input
						id="username"
						autoComplete="username"
						placeholder="admin"
						className="mt-1.5 h-9.5"
						aria-invalid={!!form.formState.errors.username}
						{...form.register("username")}
					/>
					<FieldError message={form.formState.errors.username?.message} />
				</div>

				<div>
					<Label htmlFor="password">密码</Label>
					<div className="relative mt-1.5">
						<Input
							id="password"
							type={showPassword ? "text" : "password"}
							autoComplete="current-password"
							placeholder="请输入管理密码"
							className="h-9.5 pr-9"
							aria-invalid={!!form.formState.errors.password}
							{...form.register("password")}
						/>
						<button
							type="button"
							aria-label={showPassword ? "隐藏密码" : "显示密码"}
							className="text-muted-foreground hover:text-foreground absolute top-1/2 right-2.5 -translate-y-1/2 cursor-pointer"
							onClick={() => setShowPassword((value) => !value)}
						>
							{showPassword ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
						</button>
					</div>
					<FieldError message={form.formState.errors.password?.message} />
				</div>

				<Button type="submit" className="h-9.5 w-full" isLoading={submitting}>
					登录控制台
				</Button>
			</form>

			{oidcEnabled ? (
				<Button variant="outline" className="mt-3 h-9.5 w-full" asChild>
					<a href="/api/oidc/login">使用 OIDC 登录</a>
				</Button>
			) : null}

			<div className="bg-muted/50 mt-6 flex items-start gap-2 rounded-sm border p-3">
				<KeyRound className="text-muted-foreground mt-0.5 size-3.5 shrink-0" />
				<p className="text-muted-foreground text-[11px] leading-relaxed">
					首次启动默认账号为 <span className="text-foreground font-mono">admin</span> /{" "}
					<span className="text-foreground font-mono">admin123</span>。请通过{" "}
					<span className="font-mono">KKREPO_ADMIN_USER</span> /{" "}
					<span className="font-mono">KKREPO_ADMIN_PASSWORD</span> 环境变量修改。
				</p>
			</div>
		</AuthLayout>
	);
}
