import type { ReactNode } from "react";

export function PageHeader({
	title,
	description,
	actions,
}: {
	title: string;
	description?: ReactNode;
	actions?: ReactNode;
}) {
	return (
		<div className="mb-6 flex flex-wrap items-start justify-between gap-3">
			<div>
				<h1 className="text-xl font-semibold tracking-tight">{title}</h1>
				{description ? <p className="text-muted-foreground mt-1.5 max-w-2xl text-sm">{description}</p> : null}
			</div>
			{actions ? <div className="flex items-center gap-2">{actions}</div> : null}
		</div>
	);
}
