/** Browser client for api/openapi.yaml. No runtime dependencies or token persistence. */
export type Direction = 'backend' | 'frontend' | 'mobile' | 'devops' | 'ml' | 'analytics' | 'security' | 'management' | 'design';
export type OpportunityType = 'course' | 'internship' | 'event';
export type Experience = 0 | 1 | 2 | 3 | null;
export type Format = 'online' | 'offline' | 'hybrid' | 'unknown';
export interface Selection {
  directions: Array<Direction | 'product'>; // product is a legacy alias for management/product.
  profiles?: { mobile?: 'android' | 'ios' | 'flutter' | 'mobile-all'; management?: 'product' | 'project' | 'ba' | 'management-all' };
}
export interface Answers extends Selection {
  goals?: Array<OpportunityType | 'explore'>;
  education_stage?: 'vocational' | 'undergraduate' | 'graduate' | 'alumni' | 'other' | 'prefer_not_to_say';
  study_year?: number;
  preferred_format?: 'online' | 'offline' | 'both';
  preferred_cities?: string[];
  any_city?: boolean;
  other_city?: string;
  backend_languages?: string[];
  skills?: Record<string, Experience>;
}
export interface DraftInput extends Answers { current_step: number }
export interface Document extends Answers {
  schema_version: 1;
  questionnaire_version: string;
  selection_version: 1;
  question_ids: string[];
}
export interface SavedDraft extends Document { current_step: number; base_profile_revision?: number }
export interface Profile extends Document { skills: Record<string, Experience>; completed_at: string }
export interface Session { token: string; expires_at: string; user_id: string }
export interface UserState { profile: Profile | null; profile_revision: number; draft: SavedDraft | null; draft_revision: number }
export interface DraftResponse { draft: SavedDraft | null; revision: number }
export interface Completion { profile: Profile; revision: number }
export interface Question { id: string; direction_id: Direction; text: string }
export interface Preview {
  questionnaire_version: string;
  selection_version: 1;
  directions: Direction[];
  profiles?: Selection['profiles'];
  question_count: 8 | 12 | 16;
  questions: Question[];
}
export interface Questionnaire {
  questionnaire_version: string;
  selection_version: 1;
  directions: Array<{ id: Direction; title: string; core_question_ids: string[] }>;
  profiles: Array<{ id: string; title: string; direction_id: Direction; question_ids?: string[]; selection_rule?: string }>;
  answer_scale: Array<{ value: Experience; label: string }>;
  goals: string[];
  education_stages: string[];
  preferred_formats: string[];
  backend_languages: string[];
  general_questions: Array<{ id: string; text: string }>;
}
export interface Opportunity {
  id: string;
  type: OpportunityType;
  organizer_id: string;
  title: string;
  summary: string;
  description: string;
  directions: Direction[];
  specializations?: string[];
  format: Format;
  fully_remote_allowed?: boolean;
  cities: string[] | null;
  education_requirements: { stages?: string[]; study_year_min?: number; study_year_max?: number; basis: string } | null;
  requirements_text: string | null;
  skill_requirements: Array<{ skill_id: string; min_experience: number; basis: string }> | null;
  no_prerequisites: boolean | null;
  details: {
    course?: { price_text?: string; duration_text?: string };
    internship?: { compensation_text?: string; duration_text?: string };
    event?: { venue?: string; registration_text?: string };
  };
  starts_at: string | null;
  ends_at: string | null;
  deadline_at: string | null;
  source_url: string;
  action_url: string;
  checked_at: string;
  publication_status: 'draft' | 'published' | 'archived';
  availability: 'open' | 'closed' | 'unknown';
  is_demo: boolean;
  effective_status: 'active' | 'closed';
  availability_hint?: 'check_source';
  reasons: Array<{ code: 'direction_match' | 'goal_match' | 'no_prerequisites' | 'requirements_unknown'; ref_id?: string }>;
}
export interface CatalogResult {
  catalog_version: string;
  profile_revision: number;
  ranking_version: 1;
  evaluated_at: string;
  has_direction_matches: boolean;
  items: Opportunity[];
}
export interface OpportunityResult extends Omit<CatalogResult, 'items' | 'has_direction_matches'> { item: Opportunity }
export interface FavoritesResult {
  catalog_version: string;
  profile_revision: number;
  favorites_revision: number;
  evaluated_at: string;
  items: Array<{ id: string; effective_status: 'active' | 'closed' | 'unavailable'; item: Opportunity | null }>;
}
export interface ErrorDetail { code: string; message: string; fields?: Record<string, string>; request_id: string }
export class ApiError extends Error {
  constructor(readonly status: number, readonly detail: ErrorDetail) {
    super(detail.message);
    this.name = 'ApiError';
  }
}

export class NavigatorClient {
  private token: string | undefined;
  private readonly base: string;
  constructor(baseUrl = '/api/v1', private readonly fetcher: typeof fetch = globalThis.fetch.bind(globalThis)) {
    this.base = baseUrl.replace(/\/$/, '');
  }

  /** For a session obtained by the caller. Keep tokens only in memory. */
  setSession(token: string | undefined): void { this.token = token }

  async login(initData: string, signal?: AbortSignal): Promise<Session> {
    // A failed login must not leave a previous identity active.
    this.token = undefined;
    const session = await this.request<Session>('/auth/max', 'POST', { init_data: initData }, signal);
    this.token = session.token;
    return session;
  }
  async logout(signal?: AbortSignal): Promise<void> {
    await this.request<void>('/auth/session', 'DELETE', undefined, signal);
    this.token = undefined;
  }
  async deleteData(signal?: AbortSignal): Promise<void> {
    await this.request<void>('/me/data', 'DELETE', undefined, signal);
    this.token = undefined;
  }
  me(signal?: AbortSignal): Promise<UserState> { return this.request('/me', 'GET', undefined, signal) }
  questionnaire(signal?: AbortSignal): Promise<Questionnaire> { return this.request('/questionnaire', 'GET', undefined, signal) }
  preview(selection: Selection, signal?: AbortSignal): Promise<Preview> { return this.request('/questionnaire/preview', 'POST', selection, signal) }
  draft(signal?: AbortSignal): Promise<DraftResponse> { return this.request('/me/draft', 'GET', undefined, signal) }
  saveDraft(revision: number, draft: DraftInput, signal?: AbortSignal): Promise<DraftResponse> {
    return this.request('/me/draft', 'PUT', { revision, draft }, signal);
  }
  discardDraft(signal?: AbortSignal): Promise<void> { return this.request('/me/draft', 'DELETE', undefined, signal) }
  /** Generate a UUID once per completion attempt; reuse it with the same revision on retries. */
  complete(draftRevision: number, idempotencyKey: string, signal?: AbortSignal): Promise<Completion> {
    return this.request('/me/onboarding/complete', 'POST', { draft_revision: draftRevision }, signal, { 'Idempotency-Key': idempotencyKey });
  }
  recommendations(signal?: AbortSignal): Promise<CatalogResult> { return this.request('/recommendations', 'GET', undefined, signal) }
  opportunities(filters: { type?: OpportunityType; direction?: Direction; format?: Format } = {}, signal?: AbortSignal): Promise<CatalogResult> {
    const params = new URLSearchParams();
    for (const [key, value] of Object.entries(filters)) if (value !== undefined) params.set(key, value);
    return this.request('/opportunities' + (params.size ? '?' + params.toString() : ''), 'GET', undefined, signal);
  }
  opportunity(id: string, signal?: AbortSignal): Promise<OpportunityResult> { return this.request('/opportunities/' + encodeURIComponent(id), 'GET', undefined, signal) }
  favorites(signal?: AbortSignal): Promise<FavoritesResult> { return this.request('/me/favorites', 'GET', undefined, signal) }
  addFavorite(id: string, signal?: AbortSignal): Promise<void> { return this.request('/me/favorites/' + encodeURIComponent(id), 'PUT', undefined, signal) }
  removeFavorite(id: string, signal?: AbortSignal): Promise<void> { return this.request('/me/favorites/' + encodeURIComponent(id), 'DELETE', undefined, signal) }

  private async request<T>(path: string, method: string, body?: unknown, signal?: AbortSignal, extraHeaders: Record<string, string> = {}): Promise<T> {
    const headers: Record<string, string> = { Accept: 'application/json', ...extraHeaders };
    if (this.token) headers.Authorization = 'Bearer ' + this.token;
    if (body !== undefined) headers['Content-Type'] = 'application/json';
    const response = await this.fetcher(this.base + path, {
      method, headers, body: body === undefined ? undefined : JSON.stringify(body), signal,
      credentials: 'omit', cache: 'no-store', redirect: 'error',
    });
    if (!response.ok) {
      if (response.status === 401) this.token = undefined;
      const payload = await response.json().catch(() => null) as { error?: ErrorDetail } | null;
      throw new ApiError(response.status, payload?.error ?? {
        code: 'HTTP_ERROR', message: 'HTTP ' + response.status,
        request_id: response.headers.get('X-Request-ID') ?? '',
      });
    }
    if (response.status === 204) return undefined as T;
    return await response.json() as T;
  }
}
