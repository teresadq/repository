import * as React from "react";

import { cn } from "@/lib/utils";

export function Textarea({ className, ...props }: React.ComponentProps<"textarea">) {
	return (
		<textarea
			data-slot="textarea"
			className={cn(
				"border-input placeholder:text-muted-foreground aria-invalid:border-destructive dark:bg-input/30 focus-visible:border-ring focus-visible:ring-ring/30 flex field-sizing-content min-h-20 w-full rounded-sm border bg-transparent px-3 py-2 text-base shadow-none transition-[color,box-shadow] outline-none focus-visible:ring-2 disabled:cursor-not-allowed disabled:opacity-50 md:text-sm",
				className,
			)}
			{...props}
		/>
	);
}
