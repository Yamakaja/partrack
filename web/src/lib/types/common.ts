export interface PaginatedResponse<T> {
  total: number;
  limit: number;
  offset: number;
  items: T[];
}

export interface ApiError {
  error: string;
  code: string;
}
