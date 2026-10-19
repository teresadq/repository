import { ApiError, type Repository, type RepositoryInput } from "@/lib/api";

export function updatePayload(repo: Repository, patch: Partial<RepositoryInput>): RepositoryInput {
	return {
		recipe: repo.recipe,
		online: repo.online,
		description: repo.description,
		hosted: repo.hosted ? { writePolicy: repo.hosted.writePolicy } : undefined,
		proxy: repo.proxy
			? {
					remoteUrl: repo.proxy.remoteUrl,
					contentMaxAgeMinutes: repo.proxy.contentMaxAgeMinutes,
					metadataMaxAgeMinutes: repo.proxy.metadataMaxAgeMinutes,
					autoBlock: repo.proxy.autoBlock,
				}
			: undefined,
		group: repo.group ? { memberNames: repo.group.memberNames } : undefined,
		...patch,
	};
}

export function errorMessage(error: unknown): string {
	if (error instanceof ApiError || error instanceof Error) return error.message;
	return "未知错误";
}
