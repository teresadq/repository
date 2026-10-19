import { useCallback, useEffect, useRef, useState } from "react";

import { ApiError } from "@/lib/api";

type ApiState<T> = {
	data?: T;
	loading: boolean;
	error?: string;
};

/**
 * Minimal data-loading hook. It keeps the loader in a ref so callers can pass
 * inline closures without causing an effect loop; pass real dependencies in
 * `deps` when the request should re-run.
 */
export function useApi<T>(loader: () => Promise<T>, deps: unknown[] = []) {
	const loaderRef = useRef(loader);
	loaderRef.current = loader;
	const [state, setState] = useState<ApiState<T>>({ loading: true });
	const [nonce, setNonce] = useState(0);

	useEffect(() => {
		let active = true;
		setState((previous) => ({ ...previous, loading: true, error: undefined }));
		loaderRef.current().then(
			(data) => {
				if (active) setState({ data, loading: false });
			},
			(error: unknown) => {
				if (!active) return;
				if (error instanceof ApiError && error.status === 401) {
					window.location.assign("/login");
					return;
				}
				setState({ loading: false, error: error instanceof Error ? error.message : "请求失败" });
			},
		);
		return () => {
			active = false;
		};
		// eslint-disable-next-line react-hooks/exhaustive-deps
	}, [...deps, nonce]);

	const reload = useCallback(() => setNonce((value) => value + 1), []);
	return { ...state, reload };
}
