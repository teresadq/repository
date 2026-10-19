export type RepositoryType = "hosted" | "proxy" | "group";

export type HostedSettings = { writePolicy: "ALLOW" | "DENY" };

export type ProxySettings = {
	remoteUrl: string;
	contentMaxAgeMinutes: number;
	metadataMaxAgeMinutes: number;
	autoBlock: boolean;
	remoteUsername?: string;
	remotePassword?: string;
	remoteBearerToken?: string;
};

export type GroupSettings = { memberNames: string[] };

export type ProxyState = {
	failCount: number;
	blocked: boolean;
	blockedUntil?: string;
	lastError?: string;
	lastErrorAt?: string;
	lastSuccessAt?: string;
};

export type RepoStats = {
	components: number;
	assets: number;
	bytes: number;
	lastUpdated: string;
};

export type Repository = {
	name: string;
	recipe: string;
	format: string;
	type: RepositoryType;
	online: boolean;
	description: string;
	url: string;
	hosted?: HostedSettings;
	proxy?: ProxySettings;
	group?: GroupSettings;
	createdAt: string;
	updatedAt: string;
	stats: RepoStats;
	proxyState?: ProxyState;
};

export type Component = {
	module: string;
	namespace?: string;
	format?: string;
	version: string;
	kind: string;
	time: string;
	createdAt: string;
	updatedAt: string;
	assets: number;
	bytes: number;
	archiveSize: number;
	archiveSha?: string;
	downloads: Record<string, string>;
	attributes?: Record<string, string>;
};

export type Asset = {
	path: string;
	format?: string;
	namespace?: string;
	module: string;
	version?: string;
	kind: string;
	contentType: string;
	size: number;
	sha256: string;
	source: string;
	url: string;
	createdAt: string;
	updatedAt: string;
	attributes?: Record<string, string>;
};

export type BrowseEntry = {
	name: string;
	path: string;
	type: "directory" | "file";
	size?: number;
	count?: number;
	kind?: string;
	contentType?: string;
	source?: string;
	updatedAt?: string;
	url?: string;
};

export type Activity = {
	repository: string;
	repositoryType: RepositoryType;
	format?: string;
	module: string;
	version?: string;
	kind: string;
	path: string;
	size: number;
	source: string;
	updatedAt: string;
};

export type DailyPoint = { date: string; assets: number; bytes: number };

export type ModuleUsage = { module: string; versions: number; bytes: number };

export type Overview = {
	repositories: number;
	online: number;
	hosted: number;
	proxy: number;
	groups: number;
	components: number;
	assets: number;
	bytes: number;
	daily: DailyPoint[];
	topModules: ModuleUsage[];
};

export type OverviewResponse = {
	overview: Overview;
	repositories: Repository[];
	recent: Activity[];
	uptimeSeconds: number;
};

export type StatusResponse = {
	status: string;
	version: string;
	goVersion: string;
	uptimeSeconds: number;
	dataDir: string;
	repositories: Array<{
		name: string;
		type: RepositoryType;
		online: boolean;
		proxyState?: ProxyState;
	}>;
	overview: Overview;
};

export type Principal = {
	username: string;
	name?: string;
	anonymous?: boolean;
	privileges?: string[];
};

export type SessionResponse = {
	authenticated: boolean;
	user: Principal | null;
};

export type Recipe = { name: string; format: string; type: RepositoryType; label: string };

export type UserAccount = {
	username: string;
	name: string;
	email?: string;
	roles: string[];
	groups: string[];
	active: boolean;
	createdAt: string;
	updatedAt: string;
};

export type Role = {
	name: string;
	description?: string;
	privileges: string[];
	builtIn?: boolean;
	createdAt: string;
	updatedAt: string;
};

export type SecurityGroup = {
	name: string;
	description?: string;
	roles: string[];
	builtIn?: boolean;
	createdAt: string;
	updatedAt: string;
};

export type ApiToken = {
	id: string;
	name: string;
	username: string;
	prefix: string;
	scopes?: string[];
	expiresAt?: string;
	createdAt: string;
	lastUsedAt?: string;
	token?: string;
};

export type AuditEntry = {
	id: number;
	at: string;
	user: string;
	ip?: string;
	action: string;
	repository?: string;
	format?: string;
	path?: string;
	status?: number;
	message?: string;
};

export type RequestLogEntry = {
	at: string;
	method: string;
	path: string;
	repository?: string;
	format?: string;
	status: number;
	bytes: number;
	durationMs: number;
	user?: string;
	ip?: string;
};

export type AnonymousSettings = { enabled: boolean; roles: string[] };

export type CleanupPolicy = {
	name: string;
	repository: string;
	format: string;
	enabled: boolean;
	pattern?: string;
	publishedOlderThanDays?: number;
	lastDownloadedOlderThanDays?: number;
	retainCount?: number;
	createdAt: string;
	updatedAt: string;
};

export type CleanupItem = {
	path: string;
	module?: string;
	version?: string;
	size: number;
	reason: string;
};

export type CleanupRun = {
	id: string;
	policy: string;
	at: string;
	dryRun: boolean;
	scanned: number;
	matched: number;
	deleted: number;
	bytes: number;
	items: CleanupItem[];
	error?: string;
};

export type BackupImportResult = {
	mode: string;
	repositories: number;
	skipped: number;
	assets: number;
	blobs: number;
	stateVersion: number;
};

export type ContentSelector = {
	name: string;
	description?: string;
	expression: string;
	createdAt: string;
	updatedAt: string;
};

export type RepositoryInput = {
	name?: string;
	recipe?: string;
	online?: boolean;
	description?: string;
	hosted?: { writePolicy: string };
	proxy?: {
		remoteUrl?: string;
		contentMaxAgeMinutes?: number;
		metadataMaxAgeMinutes?: number;
		autoBlock?: boolean;
		remoteUsername?: string;
		remotePassword?: string;
		remoteBearerToken?: string;
	};
	group?: { memberNames: string[] };
};

export class ApiError extends Error {
	status: number;

	constructor(status: number, message: string) {
		super(message);
		this.status = status;
	}
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
	const response = await fetch(path, {
		credentials: "same-origin",
		...init,
		headers: {
			...(init?.body ? { "Content-Type": "application/json" } : {}),
			...init?.headers,
		},
	});
	if (!response.ok) {
		let message = response.statusText;
		try {
			const body = (await response.json()) as { message?: string };
			if (body.message) message = body.message;
		} catch {
			// keep status text
		}
		throw new ApiError(response.status, message);
	}
	if (response.status === 204) {
		return undefined as T;
	}
	return (await response.json()) as T;
}

export const api = {
	session: () => request<SessionResponse>("/api/session"),
	authOptions: () => request<{ local: boolean; oidc: boolean }>("/api/auth/options"),
	recipes: () => request<{ items: Recipe[] }>("/api/recipes"),
	users: () => request<{ items: UserAccount[] }>("/api/users"),
	createUser: (input: { username: string; name?: string; email?: string; password: string; roles: string[]; groups: string[]; active?: boolean }) =>
		request<UserAccount>("/api/users", { method: "POST", body: JSON.stringify(input) }),
	updateUser: (username: string, input: { name?: string; email?: string; password?: string; roles?: string[]; groups?: string[]; active?: boolean }) =>
		request<UserAccount>(`/api/users/${encodeURIComponent(username)}`, { method: "PUT", body: JSON.stringify(input) }),
	deleteUser: (username: string) =>
		request<void>(`/api/users/${encodeURIComponent(username)}`, { method: "DELETE" }),
	roles: () => request<{ items: Role[] }>("/api/roles"),
	createRole: (input: { name: string; description?: string; privileges: string[] }) =>
		request<Role>("/api/roles", { method: "POST", body: JSON.stringify(input) }),
	updateRole: (name: string, input: { description?: string; privileges: string[] }) =>
		request<Role>(`/api/roles/${encodeURIComponent(name)}`, { method: "PUT", body: JSON.stringify(input) }),
	deleteRole: (name: string) =>
		request<void>(`/api/roles/${encodeURIComponent(name)}`, { method: "DELETE" }),
	groups: () => request<{ items: SecurityGroup[] }>("/api/groups"),
	createGroup: (input: { name: string; description?: string; roles: string[] }) =>
		request<SecurityGroup>("/api/groups", { method: "POST", body: JSON.stringify(input) }),
	updateGroup: (name: string, input: { description?: string; roles: string[] }) =>
		request<SecurityGroup>(`/api/groups/${encodeURIComponent(name)}`, { method: "PUT", body: JSON.stringify(input) }),
	deleteGroup: (name: string) =>
		request<void>(`/api/groups/${encodeURIComponent(name)}`, { method: "DELETE" }),
	tokens: () => request<{ items: ApiToken[] }>("/api/tokens"),
	createToken: (input: { name: string; username: string; expiresAt?: string }) =>
		request<ApiToken>("/api/tokens", { method: "POST", body: JSON.stringify(input) }),
	deleteToken: (id: string) => request<void>(`/api/tokens/${encodeURIComponent(id)}`, { method: "DELETE" }),
	audit: (params?: { q?: string; user?: string; action?: string; repository?: string; limit?: number; offset?: number }) => {
		const search = new URLSearchParams();
		Object.entries(params ?? {}).forEach(([key, value]) => {
			if (value !== undefined && value !== "") search.set(key, String(value));
		});
		const suffix = search.size > 0 ? `?${search.toString()}` : "";
		return request<{ items: AuditEntry[]; total: number; offset: number; limit: number }>(`/api/audit${suffix}`);
	},
	logs: (limit = 200) => request<{ items: RequestLogEntry[] }>(`/api/logs?limit=${limit}`),
	cleanupPolicies: () => request<{ items: CleanupPolicy[] }>("/api/cleanup/policies"),
	createCleanupPolicy: (input: { name: string; repository: string; pattern?: string; publishedOlderThanDays?: number; lastDownloadedOlderThanDays?: number; retainCount?: number; enabled?: boolean }) =>
		request<CleanupPolicy>("/api/cleanup/policies", { method: "POST", body: JSON.stringify(input) }),
	deleteCleanupPolicy: (name: string) =>
		request<void>(`/api/cleanup/policies/${encodeURIComponent(name)}`, { method: "DELETE" }),
	previewCleanup: (name: string) =>
		request<CleanupRun>(`/api/cleanup/policies/${encodeURIComponent(name)}/preview`, { method: "POST" }),
	runCleanup: (name: string) =>
		request<CleanupRun>(`/api/cleanup/policies/${encodeURIComponent(name)}/run`, { method: "POST" }),
	cleanupRuns: () => request<{ items: CleanupRun[] }>("/api/cleanup/runs"),
	uploadBackup: async (file: File, mode: string, force: boolean): Promise<BackupImportResult> => {
		const response = await fetch(`/api/backup/import?mode=${encodeURIComponent(mode)}&force=${force}`, {
			method: "POST",
			body: file,
			credentials: "same-origin",
		});
		if (!response.ok) {
			let message = response.statusText;
			try {
				const body = (await response.json()) as { message?: string };
				if (body.message) message = body.message;
			} catch {
				// keep status text
			}
			throw new ApiError(response.status, message);
		}
		return (await response.json()) as BackupImportResult;
	},
	selectors: () => request<{ items: ContentSelector[] }>("/api/content-selectors"),
	createSelector: (input: { name: string; description?: string; expression: string }) =>
		request<ContentSelector>("/api/content-selectors", { method: "POST", body: JSON.stringify(input) }),
	updateSelector: (name: string, input: { description?: string; expression: string }) =>
		request<ContentSelector>(`/api/content-selectors/${encodeURIComponent(name)}`, { method: "PUT", body: JSON.stringify(input) }),
	deleteSelector: (name: string) =>
		request<void>(`/api/content-selectors/${encodeURIComponent(name)}`, { method: "DELETE" }),
	testSelector: (input: { expression: string; format: string; repository: string; path: string }) =>
		request<{ matched: boolean }>("/api/content-selectors/test", { method: "POST", body: JSON.stringify(input) }),
	anonymousSettings: () => request<AnonymousSettings>("/api/security/anonymous"),
	updateAnonymousSettings: (input: AnonymousSettings) =>
		request<AnonymousSettings>("/api/security/anonymous", { method: "PUT", body: JSON.stringify(input) }),
	login: (username: string, password: string) =>
		request<SessionResponse>("/api/login", {
			method: "POST",
			body: JSON.stringify({ username, password }),
		}),
	logout: () => request<{ authenticated: boolean }>("/api/logout", { method: "POST" }),
	overview: () => request<OverviewResponse>("/api/overview"),
	status: () => request<StatusResponse>("/api/status"),
	repositories: () => request<{ items: Repository[] }>("/api/repositories"),
	repository: (name: string) => request<Repository>(`/api/repositories/${encodeURIComponent(name)}`),
	createRepository: (input: RepositoryInput) =>
		request<Repository>("/api/repositories", {
			method: "POST",
			body: JSON.stringify(input),
		}),
	updateRepository: (name: string, input: RepositoryInput) =>
		request<Repository>(`/api/repositories/${encodeURIComponent(name)}`, {
			method: "PUT",
			body: JSON.stringify(input),
		}),
	deleteRepository: (name: string) =>
		request<void>(`/api/repositories/${encodeURIComponent(name)}`, { method: "DELETE" }),
	components: (name: string, params?: { q?: string; module?: string; limit?: number; offset?: number }) => {
		const search = new URLSearchParams();
		if (params?.q) search.set("q", params.q);
		if (params?.module) search.set("module", params.module);
		if (params?.limit) search.set("limit", String(params.limit));
		if (params?.offset) search.set("offset", String(params.offset));
		const suffix = search.size > 0 ? `?${search.toString()}` : "";
		return request<{ items: Component[]; total: number; offset: number; limit: number }>(
			`/api/repositories/${encodeURIComponent(name)}/components${suffix}`,
		);
	},
	browse: (name: string, path?: string) => {
		const search = new URLSearchParams();
		if (path) search.set("path", path);
		const suffix = search.size > 0 ? `?${search.toString()}` : "";
		return request<{ path: string; entries: BrowseEntry[]; total: number }>(
			`/api/repositories/${encodeURIComponent(name)}/browse${suffix}`,
		);
	},
	assets: (name: string, params?: { q?: string; limit?: number; offset?: number }) => {
		const search = new URLSearchParams();
		if (params?.q) search.set("q", params.q);
		if (params?.limit) search.set("limit", String(params.limit));
		if (params?.offset) search.set("offset", String(params.offset));
		const suffix = search.size > 0 ? `?${search.toString()}` : "";
		return request<{ items: Asset[]; total: number; offset: number; limit: number }>(
			`/api/repositories/${encodeURIComponent(name)}/assets${suffix}`,
		);
	},
};
