import { ArrowUpDown, ChevronLeft, ChevronRight } from "lucide-react";
import { useEffect, useMemo, useState, type ReactNode } from "react";

import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { cn } from "@/lib/utils";

export type Column<T> = {
	key: string;
	header: ReactNode;
	cell: (row: T) => ReactNode;
	sortable?: boolean;
	sortValue?: (row: T) => string | number;
	className?: string;
	headClassName?: string;
};

type DataTableProps<T extends { id: string }> = {
	columns: Column<T>[];
	rows: T[];
	pageSize?: number;
	selectable?: boolean;
	selected?: Set<string>;
	onSelectedChange?: (next: Set<string>) => void;
	emptyText?: string;
	onRowClick?: (row: T) => void;
	rowClassName?: (row: T) => string | undefined;
	resetKey?: string;
};

export function DataTable<T extends { id: string }>({
	columns,
	rows,
	pageSize = 8,
	selectable = false,
	selected = new Set<string>(),
	onSelectedChange,
	emptyText = "暂无数据",
	onRowClick,
	rowClassName,
	resetKey,
}: DataTableProps<T>) {
	const [sort, setSort] = useState<{ key: string; dir: "asc" | "desc" } | null>(null);
	const [page, setPage] = useState(1);

	useEffect(() => {
		setPage(1);
	}, [resetKey, rows.length]);

	const sorted = useMemo(() => {
		if (!sort) return rows;
		const column = columns.find((item) => item.key === sort.key);
		if (!column?.sortValue) return rows;
		return [...rows].sort((a, b) => {
			const left = column.sortValue!(a);
			const right = column.sortValue!(b);
			const result = typeof left === "number" && typeof right === "number" ? left - right : String(left).localeCompare(String(right));
			return sort.dir === "asc" ? result : -result;
		});
	}, [rows, sort, columns]);

	const pageCount = Math.max(1, Math.ceil(sorted.length / pageSize));
	const currentPage = Math.min(page, pageCount);
	const pageRows = sorted.slice((currentPage - 1) * pageSize, currentPage * pageSize);

	const allSelected = pageRows.length > 0 && pageRows.every((row) => selected.has(row.id));
	const someSelected = pageRows.some((row) => selected.has(row.id));

	function updateSelection(next: Set<string>) {
		onSelectedChange?.(next);
	}

	function toggleAll(checked: boolean) {
		const next = new Set(selected);
		for (const row of pageRows) {
			if (checked) next.add(row.id);
			else next.delete(row.id);
		}
		updateSelection(next);
	}

	function toggleOne(row: T, checked: boolean) {
		const next = new Set(selected);
		if (checked) next.add(row.id);
		else next.delete(row.id);
		updateSelection(next);
	}

	function toggleSort(key: string) {
		setSort((current) =>
			current?.key === key ? { key, dir: current.dir === "asc" ? "desc" : "asc" } : { key, dir: "asc" },
		);
	}

	return (
		<div className="rounded-lg border">
			<Table>
				<TableHeader>
					<TableRow className="hover:bg-transparent">
						{selectable && (
							<TableHead className="w-10 pl-3">
								<Checkbox
									aria-label="全选当前页"
									checked={allSelected ? true : someSelected ? "indeterminate" : false}
									onCheckedChange={(checked) => toggleAll(checked === true)}
								/>
							</TableHead>
						)}
						{columns.map((column) => (
							<TableHead key={column.key} className={cn(column.headClassName)}>
								{column.sortable && column.sortValue ? (
									<button
										type="button"
										className="flex cursor-pointer items-center gap-1.5"
										onClick={() => toggleSort(column.key)}
									>
										{column.header}
										<ArrowUpDown
											className={cn(
												"size-3",
												sort?.key === column.key ? "text-foreground" : "text-muted-foreground/50",
											)}
										/>
									</button>
								) : (
									column.header
								)}
							</TableHead>
						))}
					</TableRow>
				</TableHeader>
				<TableBody>
					{pageRows.map((row) => (
						<TableRow
							key={row.id}
							data-state={selected.has(row.id) ? "selected" : undefined}
							onClick={onRowClick ? () => onRowClick(row) : undefined}
							className={cn(onRowClick && "cursor-pointer", rowClassName?.(row))}
						>
							{selectable && (
								<TableCell className="pl-3" onClick={(event) => event.stopPropagation()}>
									<Checkbox
										aria-label="选择行"
										checked={selected.has(row.id)}
										onCheckedChange={(checked) => toggleOne(row, checked === true)}
									/>
								</TableCell>
							)}
							{columns.map((column) => (
								<TableCell key={column.key} className={column.className}>
									{column.cell(row)}
								</TableCell>
							))}
						</TableRow>
					))}
					{pageRows.length === 0 && (
						<TableRow>
							<TableCell
								colSpan={columns.length + (selectable ? 1 : 0)}
								className="text-muted-foreground h-28 text-center text-sm"
							>
								{emptyText}
							</TableCell>
						</TableRow>
					)}
				</TableBody>
			</Table>

			<div className="flex flex-wrap items-center justify-between gap-3 border-t px-3 py-2.5">
				<p className="text-muted-foreground text-xs">
					{sorted.length === 0 ? 0 : (currentPage - 1) * pageSize + 1}-
					{Math.min(currentPage * pageSize, sorted.length)} / {sorted.length} 条
				</p>
				<div className="flex items-center gap-1">
					<Button
						variant="ghost"
						size="icon"
						className="size-7"
						aria-label="上一页"
						disabled={currentPage <= 1}
						onClick={() => setPage((value) => Math.max(1, value - 1))}
					>
						<ChevronLeft className="size-4" />
					</Button>
					<span className="text-muted-foreground px-2 text-xs">
						第 {currentPage} / {pageCount} 页
					</span>
					<Button
						variant="ghost"
						size="icon"
						className="size-7"
						aria-label="下一页"
						disabled={currentPage >= pageCount}
						onClick={() => setPage((value) => Math.min(pageCount, value + 1))}
					>
						<ChevronRight className="size-4" />
					</Button>
				</div>
			</div>
		</div>
	);
}
