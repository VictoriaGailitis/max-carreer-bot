#!/usr/bin/env python3
"""Collect public VK opportunities into the catalog JSON accepted by cmd/ops."""
from __future__ import annotations

import argparse
from datetime import datetime, timezone
import hashlib
from html.parser import HTMLParser
import json
import os
from pathlib import Path
import re
import sys
import time
import unicodedata
from urllib.error import HTTPError, URLError
from urllib.parse import urljoin, urlsplit, urlunsplit
from urllib.request import HTTPRedirectHandler, Request, build_opener

ROOT = Path(__file__).resolve().parents[2]
SOURCES = {
    'courses': 'https://education.vk.company/centrum/video-courses',
    'internships': 'https://internship.vk.company/internship',
    'vacancies': 'https://internship.vk.company/vacancy',
    'projects': 'https://education.vk.company/education_projects',
    'practice': 'https://education.vk.company/program/vk_education_practice',
    'olympiads': 'https://education.vk.company/centrum/olymp',
}
FETCH_HOSTS = {'education.vk.company', 'internship.vk.company', 'tilda-embed.tech-mail.ru'}
ACTION_HOSTS = {'education.vk.company', 'internship.vk.company', 'vk.com', 'ai.edu.gov.ru'}
DIRECTIONS = ['backend', 'frontend', 'mobile', 'devops', 'ml', 'analytics', 'security', 'management', 'design']
MAX_RESPONSE = 5 << 20


class SeedError(RuntimeError):
    pass


def safe_url(value: str, hosts: set[str]) -> str:
    url = urlsplit(value)
    if url.scheme != 'https' or url.hostname not in hosts or url.username or url.password or url.port or url.query:
        raise SeedError('Unexpected source URL')
    return urlunsplit((url.scheme, url.netloc, url.path, '', ''))


class Redirects(HTTPRedirectHandler):
    max_redirections = 5

    def redirect_request(self, req, fp, code, msg, headers, newurl):
        safe_url(newurl, FETCH_HOSTS)
        return super().redirect_request(req, fp, code, msg, headers, newurl)


class Fetcher:
    def __init__(self, timeout: float = 20, delay: float = .3):
        self.timeout, self.delay = timeout, delay
        self.opener = build_opener(Redirects())
        self.cache: dict[str, str] = {}
        self.sources: list[dict] = []

    def __call__(self, url: str) -> str:
        url = safe_url(url, FETCH_HOSTS)
        if url in self.cache:
            return self.cache[url]
        for attempt in range(3):
            try:
                time.sleep(self.delay)
                request = Request(url, headers={'User-Agent': 'VKCatalogSeed/1.0', 'Accept': 'text/html'})
                with self.opener.open(request, timeout=self.timeout) as response:
                    safe_url(response.geturl(), FETCH_HOSTS)
                    raw = response.read(MAX_RESPONSE + 1)
                    if len(raw) > MAX_RESPONSE or 'text/html' not in response.headers.get('Content-Type', ''):
                        raise SeedError('Unexpected source response')
                    text = raw.decode('utf-8')
                self.cache[url] = text
                # Only source URL/hash are retained; no cookies, OAuth state or user data.
                self.sources.append({'url': url, 'sha256': hashlib.sha256(raw).hexdigest()})
                return text
            except HTTPError as error:
                if error.code not in [408, 429, 500, 502, 503, 504] or attempt == 2:
                    raise SeedError(f'HTTP {error.code}: {url}') from None
            except (URLError, TimeoutError, OSError) as error:
                if attempt == 2:
                    raise SeedError(f'Cannot fetch {url}: {type(error).__name__}') from None
            time.sleep(1 + attempt)
        raise SeedError(f'Cannot fetch {url}')


class Document(HTMLParser):
    def __init__(self, html: str):
        super().__init__(convert_charrefs=True)
        self.in_next = False
        self.hidden = 0
        self.next_chunks: list[str] = []
        self.texts: list[str] = []
        self.frames: list[str] = []
        self.feed(html)

    def handle_starttag(self, tag, attributes):
        attrs = dict(attributes)
        if tag in ['script', 'style']:
            self.hidden += 1
        if tag == 'script' and attrs.get('id') == '__NEXT_DATA__':
            self.in_next = True
        if tag == 'iframe' and attrs.get('src'):
            self.frames.append(attrs['src'])

    def handle_endtag(self, tag):
        if tag == 'script':
            self.in_next = False
        if tag in ['script', 'style']:
            self.hidden = max(0, self.hidden - 1)

    def handle_data(self, value):
        if self.in_next:
            self.next_chunks.append(value)
        elif not self.hidden and value.strip():
            self.texts.append(value)

    def page(self) -> dict:
        try:
            page = json.loads(''.join(self.next_chunks))['props']['pageProps']['page']
        except (ValueError, KeyError, TypeError):
            raise SeedError('VK page no longer contains the expected public Next.js data') from None
        if not isinstance(page, dict):
            raise SeedError('Unexpected VK page schema')
        return page


def clean(value, limit: int = 10000) -> str:
    if not isinstance(value, str):
        return ''
    value = value.replace('\\xa0', ' ').replace('\\n', ' ')
    value = ' '.join(Document(value).texts)
    value = ' '.join(''.join(c for c in value if not unicodedata.category(c).startswith('C')).split())
    if len(value) > limit:
        value = value[:limit].rsplit(' ', 1)[0].rstrip(' ,;')
    return value.strip()


def sections(*values, limit=10000):
    return clean(' '.join(clean(value) for value in values if value), limit)


def require_list(page: dict, key: str) -> list:
    value = page.get(key)
    if not isinstance(value, list) or any(not isinstance(item, dict) for item in value):
        raise SeedError(f'VK page schema changed: {key}')
    return value


def date(value) -> str | None:
    if not value:
        return None
    try:
        parsed = datetime.fromisoformat(value.replace('Z', '+00:00'))
        if parsed.tzinfo is None:
            raise ValueError()
    except (ValueError, AttributeError):
        raise SeedError('Invalid structured source date') from None
    return parsed.isoformat()


def classify(title: str, label: str = '') -> list[str]:
    text = (title + ' ' + label).lower()
    rules = {
        'frontend': r'frontend|фронтенд|\bhtml\b|\bcss\b|javascript|веб-интерфейс',
        'mobile': r'android|\bios\b|flutter|мобильн.*разработ',
        'devops': r'devops|sre|контейнер|kubernetes',
        'backend': r'backend|бэкенд|python|c\+\+|серверн|алгоритм|системн.*программ|олимпиад.*программ',
        'ml': r'машинн.*обуч|искусственн.*интеллект|\bml\b|нейросет|\bии\b|ранжирован',
        'analytics': r'аналит|анализ.*данн|big data|больш.*данн|data engineer',
        'security': r'безопасн|appsec|кибер',
        'management': r'менедж|продакт|product|маркетинг|smm|non-tech|гибкие навыки',
        'design': r'дизайн|креатив|digital|медиа|игр',
    }
    found = {key for key, pattern in rules.items() if re.search(pattern, text)}
    # Multi-area source labels include software development alongside other areas.
    broad_development = 'разработка' in label.lower() and len(re.split(r'[,;]|\sи\s', label)) > 1
    if (not found and 'разработка' in text) or broad_development:
        found.update(['backend', 'frontend', 'mobile', 'devops'])
    # Broad introductory programs have no single direction. This is an editorial
    # seed mapping, not an eligibility rule or a required skill level.
    return [direction for direction in DIRECTIONS if direction in found] or DIRECTIONS.copy()


def info_cards(program: dict) -> dict[str, str]:
    cards = program.get('landing', {}).get('aboutTexts', {}).get('infoCards', [])
    return {clean(card.get('title')).lower(): clean(card.get('text')) for card in cards if isinstance(card, dict)}


def card(kind: str, identifier: str, title: str, summary: str, url: str, checked_at: str) -> dict:
    title, summary = clean(title, 160), clean(summary, 500)
    if not title or not summary:
        raise SeedError(f'Empty source title/description: {url}')
    return {
        'id': identifier, 'type': kind, 'organizer_id': 'vk', 'title': title,
        'summary': summary, 'description': summary, 'directions': classify(title),
        'format': 'unknown', 'cities': [], 'education_requirements': None,
        'requirements_text': None, 'skill_requirements': [], 'no_prerequisites': None,
        'details': {}, 'starts_at': None, 'ends_at': None, 'deadline_at': None,
        'source_url': url, 'action_url': url, 'checked_at': checked_at,
        'publication_status': 'published', 'availability': 'unknown', 'is_demo': False,
    }


class Collector:
    def __init__(self, fetch, checked_at: str):
        self.fetch, self.checked_at = fetch, checked_at
        self.items: dict[str, dict] = {}
        self.mappings: list[dict] = []

    def remember(self, item: dict, label: str):
        self.items[item['id']] = item
        self.mappings.append({'id': item['id'], 'source_url': item['source_url'],
                              'source_direction': clean(label, 500), 'mapped_directions': item['directions']})

    def program(self, listing: dict | None, source_url: str, practice=False):
        page = Document(self.fetch(source_url)).page()
        program = page.get('program')
        if not isinstance(program, dict) or not isinstance(program.get('landing'), dict):
            raise SeedError(f'Program schema changed: {source_url}')
        curriculum_id = (listing or {}).get('curr_id') or program.get('curriculum_id')
        if not isinstance(curriculum_id, int):
            raise SeedError('Program has no stable curriculum ID')
        identifier = f'vk-program-{curriculum_id}'
        landing = program['landing']
        info = info_cards(program)
        kind = 'internship' if practice else 'course'
        item = card(kind, identifier, program.get('name'), program.get('description'), source_url, self.checked_at)
        text = landing.get('aboutTexts', {}).get('line1')
        item['description'] = sections(program.get('description'), text)
        label = info.get('направление') or info.get('направления') or ''
        item['directions'] = classify(program['name'], label)
        requirements = landing.get('requirementTexts', {}).get('requirementCards', [])
        audience = info.get('кто может учиться') or program.get('for_whom') or ''
        item['requirements_text'] = sections(audience, *(entry.get('text') for entry in requirements if isinstance(entry, dict)), limit=2000) or None
        source_format = info.get('формат', '').lower() or (listing or {}).get('format') or ''
        item['format'] = {'online': 'online', 'онлайн': 'online', 'offline': 'offline', 'очно': 'offline',
                          'hybrid': 'hybrid', 'смешанный': 'hybrid'}.get(source_format, 'unknown')
        item['cities'] = [clean(city.get('name') if isinstance(city, dict) else city, 80)
                          for city in (listing or {}).get('cities', [])]
        duration = info.get('длительность')
        if practice:
            item['details'] = {'internship': {'duration_text': clean(duration or 'Длительность уточняется на странице программы', 300)}}
        else:
            price = info.get('стоимость')
            item['details'] = {'course': {'price_text': clean(price or 'Стоимость уточняется на странице программы', 300)}}
            if duration:
                item['details']['course']['duration_text'] = clean(duration, 300)
        selection = program.get('selection') or {}
        item['deadline_at'] = date(selection.get('end_date'))
        item['availability'] = {'open': 'open', 'closed': 'closed'}.get(selection.get('status'), 'unknown')
        self.remember(item, label)

    def courses(self, page: dict, source_url: str):
        for listing in require_list(page, 'programs'):
            curriculum_id = listing.get('curr_id')
            if not isinstance(curriculum_id, int):
                raise SeedError('Course listing schema changed')
            if f'vk-program-{curriculum_id}' in self.items:
                continue
            self.program(listing, f'https://education.vk.company/program/{curriculum_id}')

    def internships(self, page: dict):
        for listing in require_list(page, 'vacancies'):
            if listing.get('internship_type') not in ['internship', 'junior']:
                raise SeedError('Vacancy type schema changed')
            # Permanent roles are outside this catalog; they produce no report entries.
            if listing.get('internship_type') != 'internship':
                continue
            vacancy_id = listing.get('id')
            if not isinstance(vacancy_id, int):
                raise SeedError('Internship ID schema changed')
            url = f'https://internship.vk.company/vacancy/{vacancy_id}'
            vacancy = Document(self.fetch(url)).page().get('vacancy')
            if not isinstance(vacancy, dict) or vacancy.get('internship_type') != 'internship':
                raise SeedError(f'Internship detail schema changed: {url}')
            landing = vacancy.get('landing') or {}
            tasks = landing.get('aboutTasksText', {}).get('items', [])
            about = sections(landing.get('aboutTeamText', {}).get('description'),
                             landing.get('aboutProjectText', {}).get('description'), *tasks)
            item = card('internship', f'vk-internship-{vacancy_id}', vacancy.get('title'), about or vacancy.get('title'), url, self.checked_at)
            conditions = sections(*landing.get('aboutConditionsText', {}).get('items', []))
            item['description'] = sections(about, conditions, vacancy.get('employment_verbose'))
            skills = landing.get('aboutSkillsText', {}).get('items', [])
            item['requirements_text'] = sections(*skills, limit=2000) or None
            label = vacancy.get('direction', '')
            item['directions'] = classify(vacancy['title'], label)
            item['format'] = listing.get('format') if listing.get('format') in ['online', 'offline', 'hybrid'] else 'unknown'
            cities = clean(listing.get('city'))
            item['cities'] = list(dict.fromkeys(clean(city, 80).replace('СПб', 'Санкт-Петербург')
                                                for city in cities.split(',') if clean(city) not in ['', 'Любой']))
            if item['format'] == 'hybrid' and 'других городов' in conditions.lower() and 'удалённо' in conditions.lower():
                item['fully_remote_allowed'] = True
            period = sections(vacancy.get('date_work_starts'), '—' if vacancy.get('date_work_starts') and vacancy.get('date_work_ends') else '', vacancy.get('date_work_ends'), limit=300)
            item['details'] = {'internship': {'duration_text': period or 'Длительность уточняется на странице стажировки'}}
            if vacancy.get('is_opened') is True:
                item['availability'] = 'open'
            elif vacancy.get('is_opened') is False:
                item['availability'] = 'closed'
            self.remember(item, label)

    def activities(self, page: dict, source_url: str):
        for activity in require_list(page, 'activities'):
            if not isinstance(activity.get('id'), int):
                raise SeedError('Activity ID schema changed')
            item = card('event', f"vk-activity-{activity['id']}", activity.get('name'), activity.get('description'), source_url, self.checked_at)
            item['directions'] = classify(activity['name'], activity.get('description') or '')
            item['requirements_text'] = clean(activity.get('for_whom'), 2000) or None
            item['format'] = activity.get('format') if activity.get('format') in ['online', 'offline', 'hybrid'] else 'unknown'
            item['deadline_at'] = date(activity.get('selection_end_date'))
            item['availability'] = {'open': 'open', 'closed': 'closed'}.get(activity.get('selection_status'), 'unknown')
            item['details'] = {'event': {'registration_text': clean(item['requirements_text'] or 'Условия участия на странице организатора', 300)}}
            try:
                item['action_url'] = safe_url(activity.get('external_url') or source_url, ACTION_HOSTS)
            except (SeedError, ValueError):
                item['action_url'] = source_url
            self.remember(item, '')

    def projects(self, document: Document, source_url: str):
        frames = [url for url in document.frames if urlsplit(url).hostname == 'tilda-embed.tech-mail.ru']
        if len(frames) != 1:
            raise SeedError('Educational projects iframe schema changed')
        text = [clean(value) for value in Document(self.fetch(safe_url(frames[0], FETCH_HOSTS))).texts]
        try:
            start = text.index('Решай кейсы бизнес-юнитов VK')
            end = text.index('С чем помогут кейсы?', start)
            description = sections(*text[start + 1:end])
            audience = text[text.index('Кто может участвовать в программе?') + 1]
        except (ValueError, IndexError):
            raise SeedError('Educational projects content schema changed') from None
        item = card('course', 'vk-education-projects', 'VK Education Projects', description, source_url, self.checked_at)
        item['description'] = description
        item['directions'] = classify('VK Education Projects', description)
        item['requirements_text'] = clean(audience, 2000) or None
        item['details'] = {'course': {'duration_text': 'Срок работы над кейсом уточняется на странице проекта'}}
        self.remember(item, description)

    def collect(self) -> dict:
        documents = {key: Document(self.fetch(url)) for key, url in SOURCES.items()}
        if not require_list(documents['courses'].page(), 'programs'):
            raise SeedError('Video course source unexpectedly returned no programs')
        self.courses(documents['courses'].page(), SOURCES['courses'])
        self.internships(documents['internships'].page())
        self.internships(documents['vacancies'].page())
        self.projects(documents['projects'], SOURCES['projects'])
        self.program(None, SOURCES['practice'], practice=True)
        self.courses(documents['olympiads'].page(), SOURCES['olympiads'])
        self.activities(documents['olympiads'].page(), SOURCES['olympiads'])
        if not self.items or len(self.items) > 500:
            raise SeedError('Collected catalog must contain 1–500 cards')
        return {'schema_version': 1, 'is_demo': False, 'items': [self.items[key] for key in sorted(self.items)]}


def write_json(path: Path, data):
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_name(path.name + '.tmp')
    with temporary.open('w', encoding='utf-8') as stream:
        json.dump(data, stream, ensure_ascii=False, indent=2)
        stream.write('\n')
    os.replace(temporary, path)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, default=ROOT / '.local/catalog/vk.json')
    parser.add_argument('--report', type=Path, default=ROOT / '.local/catalog/vk-report.json')
    parser.add_argument('--timeout', type=float, default=20)
    parser.add_argument('--delay', type=float, default=.3)
    args = parser.parse_args()
    if args.timeout <= 0 or args.delay < 0 or args.output.resolve() == args.report.resolve():
        parser.error('Invalid timeout, delay or output paths')
    fetcher = Fetcher(args.timeout, args.delay)
    checked = datetime.now(timezone.utc).isoformat(timespec='seconds')
    try:
        collector = Collector(fetcher, checked)
        dataset = collector.collect()
        write_json(args.output, dataset)
        write_json(args.report, {'checked_at': checked, 'sources': fetcher.sources, 'cards': collector.mappings})
    except (SeedError, OSError, ValueError) as error:
        print(f'VK collection failed: {error}', file=sys.stderr)
        return 1
    counts = {kind: sum(item['type'] == kind for item in dataset['items']) for kind in ['course', 'internship', 'event']}
    print('VK catalog collected: ' + ', '.join(f'{kind}={count}' for kind, count in counts.items()))
    print(f'Catalog: {args.output}')
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
