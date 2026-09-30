import { useId, useRef, useState } from 'react';
import { canonicalCity, suggestCities, type City } from './cities';
import { Choice } from './ui';

export function CityPicker({ selected, query, anyCity, onQuery, onChange }: {
  selected: string[]; query: string; anyCity: boolean;
  onQuery: (query: string) => void; onChange: (cities: string[], anyCity: boolean) => void;
}) {
  const id = useId();
  const input = useRef<HTMLInputElement>(null);
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(0);
  const suggestions = suggestCities(query, selected);
  const expanded = open && !anyCity && selected.length < 20;
  const activeIndex = Math.min(active, Math.max(0, suggestions.length - 1));
  const unknown = !!query.trim() && suggestions.length === 0;
  function choose(city: City) {
    onChange([...selected, city.value], false); onQuery(''); setActive(0); setOpen(false); input.current?.focus();
  }
  return <section className="city-picker">
    <label className="field" htmlFor={id}>Город России</label>
    <input ref={input} id={id} role="combobox" aria-autocomplete="list" aria-expanded={expanded}
      aria-controls={`${id}-options`} aria-activedescendant={expanded && suggestions.length ? `${id}-option-${activeIndex}` : undefined}
      aria-describedby={`${id}-hint`} aria-invalid={unknown || undefined} autoComplete="off" spellCheck={false}
      maxLength={80} placeholder="Начни вводить название" value={query} disabled={anyCity || selected.length >= 20}
      onFocus={() => setOpen(true)} onBlur={() => setOpen(false)}
      onChange={e => { onQuery(e.target.value); setActive(0); setOpen(true); }}
      onKeyDown={e => {
        if (e.key === 'ArrowDown' || e.key === 'ArrowUp') { e.preventDefault(); setOpen(true); setActive(Math.max(0, Math.min(suggestions.length - 1, activeIndex + (e.key === 'ArrowDown' ? 1 : -1)))); }
        if (e.key === 'Escape') { e.preventDefault(); setOpen(false); }
        if (e.key === 'Enter') { e.preventDefault(); if (expanded && suggestions[activeIndex]) choose(suggestions[activeIndex].city); else setOpen(true); }
      }}/>
    <p className="muted city-hint" id={`${id}-hint`} role="status">{anyCity ? 'Подойдёт очное участие в любом городе.' : selected.length >= 20 ? 'Выбрано 20 городов — это максимум.' : unknown ? 'Город не найден в справочнике. Проверь написание или попробуй другое название.' : query.trim() && suggestions[0]?.approximate ? 'Возможно, ты ищешь один из этих городов? Выбери нужный.' : 'Выбери город из подсказок. Можно добавить несколько.'}</p>
    {expanded && suggestions.length > 0 && <ul className="city-suggestions" id={`${id}-options`} role="listbox" aria-label="Подсказки городов">{suggestions.map(({ city, approximate }, index) => <li role="presentation" key={city.id}><button type="button" role="option" tabIndex={-1} id={`${id}-option-${index}`} aria-selected={index === activeIndex} className={index === activeIndex ? 'is-active' : ''} onMouseDown={e => e.preventDefault()} onClick={() => choose(city)}><strong>{city.name}</strong><span>{city.region}{approximate ? ' · похожее название' : ''}</span></button></li>)}</ul>}
    {!anyCity && selected.length > 0 && <ul className="city-selected" aria-label="Выбранные города">{selected.map(city => <li key={city}><span>{city}{!canonicalCity(city) && <small>Нужно выбрать заново из справочника</small>}</span><button type="button" aria-label={`Убрать ${city}`} onClick={() => onChange(selected.filter(value => value !== city), false)}>×</button></li>)}</ul>}
    {query.trim() && !anyCity && <button className="text-button" type="button" onClick={() => { onQuery(''); setOpen(false); }}>Очистить ввод</button>}
    <Choice checked={anyCity} onChange={() => { onQuery(''); setOpen(false); onChange([], !anyCity); }}>Любой город</Choice>
    <small className="city-source">Справочник: <a href="https://github.com/hflabs/city" target="_blank" rel="noreferrer">HFLabs</a> · <a href="https://creativecommons.org/licenses/by-sa/4.0/" target="_blank" rel="noreferrer">CC BY-SA 4.0</a></small>
  </section>;
}
