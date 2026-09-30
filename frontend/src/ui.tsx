import { useEffect, useRef, type ButtonHTMLAttributes, type ReactNode } from 'react';
import { ApiError } from '../../api/client/client';
export function Button({ children, className = '', ...props }: ButtonHTMLAttributes<HTMLButtonElement>) {
  return <button className={`button ${className}`} {...props}>{children}</button>;
}
export function Mascot({ pose = 'wave', className = '' }: { pose?: 'wave' | 'think' | 'support' | 'error-v1' | 'loading-v1'; className?: string }) {
  return <img className={`mascot ${className}`} src={`/assets/kursik-${pose}.png`} alt="" />;
}
export function Heading({ eyebrow, title, children }: { eyebrow?: string; title: string; children?: ReactNode }) {
  return <header className="heading">{eyebrow && <p className="eyebrow">{eyebrow}</p>}<h1 tabIndex={-1}>{title}</h1>{children && <p className="muted">{children}</p>}</header>;
}
export function message(error: unknown): string {
  if (error instanceof ApiError) {
    const friendly: Record<string, string> = {
      INVALID_SESSION: 'Сессия завершилась. Открой приложение заново в MAX.', INIT_DATA_EXPIRED: 'Данные входа устарели. Закрой мини-приложение и открой его заново в MAX.',
      REVISION_CONFLICT: 'Ответы изменились в другой сессии. Твои изменения остались на экране; серверный черновик не перезаписан.',
      PROFILE_OUTDATED: 'Опрос обновился. Нужно пересмотреть ответы.', PROFILE_REQUIRED: 'Сначала расскажи о себе в опросе.',
      OPPORTUNITY_UNAVAILABLE: 'Возможность больше недоступна. Обнови список.',
    };
    return friendly[error.detail.code] ?? (error.status === 503 ? 'Сервис пока недоступен. Попробуй ещё раз чуть позже.' : error.detail.message);
  }
  return error instanceof Error && error.message === 'NO_MAX' ? 'Открой мини-приложение из бота в MAX.' : 'Не удалось связаться с сервисом. Ответы на экране сохранены — попробуй ещё раз.';
}
export function ErrorNotice({ error, retry }: { error: unknown; retry?: () => void }) {
  if (!error) return null;
  return <div className="notice" role="alert"><p>{typeof error === 'string' ? error : message(error)}</p>{error instanceof ApiError && error.detail.fields && <ul>{Object.entries(error.detail.fields).map(([key, value]) => <li key={key}>{value}</li>)}</ul>}{retry && <Button className="secondary" onClick={retry}>Попробовать ещё раз</Button>}</div>;
}
export function Loading({ children = 'Загружаем…' }: { children?: ReactNode }) {
  return <div className="loading" role="status"><span className="spinner" />{children}</div>;
}
export function StatusPanel({ title, error, loading, retry, children }: { title: string; error?: unknown; loading?: boolean; retry?: () => void; children?: ReactNode }) {
  return <section className="status-panel" aria-busy={loading}>
    <div className={`status-art ${error ? 'error' : ''}`}><Mascot pose={error ? 'error-v1' : 'loading-v1'}/></div>
    <h2>{title}</h2>
    {loading ? <Loading/> : <ErrorNotice error={error}/>}
    {retry && <Button onClick={retry}>Попробовать ещё раз</Button>}
    {children}
  </section>;
}
export function Choice({ children, checked, onChange, disabled }: { children: ReactNode; checked: boolean; onChange: () => void; disabled?: boolean }) {
  return <button type="button" className={`choice ${checked ? 'chosen' : ''}`} aria-pressed={checked} onClick={onChange} disabled={disabled}><span>{children}</span>{checked && <img className="choice-check" src="/assets/selected.svg" alt=""/>}</button>;
}

export function ConfirmDialog({ title, children, onClose, busy = false }: { title: string; children: ReactNode; onClose: () => void; busy?: boolean }) {
  const ref = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    const dialog = ref.current!;
    const focused = document.activeElement as HTMLElement | null;
    dialog.showModal();
    return () => { dialog.close(); focused?.focus(); };
  }, []);
  return <dialog ref={ref} className="confirm" aria-label={title} onCancel={event => { event.preventDefault(); if (!busy) onClose(); }}><div>{children}</div></dialog>;
}
