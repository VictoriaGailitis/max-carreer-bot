#!/usr/bin/env python3
"""Offline checks for source schema changes and conservative VK normalization."""
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('vk_catalog', Path(__file__).resolve().parents[1] / 'seed/vk_catalog.py')
vk = importlib.util.module_from_spec(spec)
spec.loader.exec_module(vk)
WHEN = '2026-09-28T12:00:00+00:00'


def html(page):
    return '<script type="application/json" id="__NEXT_DATA__">' + json.dumps({
        'props': {'pageProps': {'page': page, 'vkid': {'state': 'DO_NOT_STORE'}, 'setCookie': ['DO_NOT_STORE']}}
    }) + '</script>'


class CollectorTests(unittest.TestCase):
    def test_direction_mapping_distinguishes_algorithms_from_data_analysis(self):
        self.assertEqual(vk.classify('Алгоритмы и структуры данных'), ['backend'])
        self.assertEqual(vk.classify('Введение в анализ данных'), ['analytics'])
        mapped = vk.classify('Проекты', 'Разработка, ИИ и анализ данных, Non-tech')
        self.assertIn('backend', mapped)
        self.assertIn('frontend', mapped)
        self.assertIn('analytics', mapped)

    def test_course_detail_is_enriched_and_deduplicated(self):
        program = {'id': 900, 'name': 'Тестовый курс HTML', 'description': '<b>Разработка интерфейсов</b>',
                   'landing': {'aboutTexts': {'line1': 'Учебные материалы', 'infoCards': [
                       {'title': 'Стоимость', 'text': 'Бесплатно'}, {'title': 'Длительность', 'text': '2 месяца'},
                       {'title': 'Формат', 'text': 'Онлайн'}]}, 'requirementTexts': {
                       'requirementCards': [{'text': 'Нужны основы JavaScript'}]}},
                   'selection': {'status': 'open', 'end_date': '2027-01-01T12:00:00+03:00'}}
        calls = []
        def fetch(url):
            calls.append(url)
            return html({'program': program})
        collector = vk.Collector(fetch, WHEN)
        listing = {'programs': [{'curr_id': 10}]}
        collector.courses(listing, 'https://education.vk.company/centrum/video-courses')
        collector.courses(listing, 'https://education.vk.company/centrum/olymp')
        item = collector.items['vk-program-10']
        self.assertEqual(len(calls), 1)
        self.assertEqual(item['type'], 'course')
        self.assertEqual(item['format'], 'online')
        self.assertEqual(item['details']['course']['duration_text'], '2 месяца')
        self.assertEqual(item['deadline_at'], '2027-01-01T12:00:00+03:00')
        self.assertEqual(item['directions'], ['frontend'])
        self.assertIsNone(item['no_prerequisites'])
        self.assertEqual(item['skill_requirements'], [])
        self.assertNotIn('DO_NOT_STORE', json.dumps(collector.items))
        self.assertNotIn('DO_NOT_STORE', json.dumps(collector.mappings))

    def test_internship_keeps_months_as_text_and_remote_evidence(self):
        vacancy = {'id': 11, 'title': 'Тестовый Python-разработчик', 'internship_type': 'internship',
                   'direction': 'Разработка', 'date_work_starts': 'октябрь 2026', 'date_work_ends': 'март 2027',
                   'is_opened': True, 'landing': {'aboutTeamText': {'description': 'Создавай API'},
                   'aboutSkillsText': {'items': ['Основы Python']},
                   'aboutConditionsText': {'items': ['Жители других городов стажируются удалённо.']}}}
        collector = vk.Collector(lambda _: html({'vacancy': vacancy}), WHEN)
        collector.internships({'vacancies': [{'id': 11, 'internship_type': 'internship', 'city': 'Любой', 'format': 'hybrid'}]})
        item = collector.items['vk-internship-11']
        self.assertTrue(item['fully_remote_allowed'])
        self.assertEqual(item['cities'], [])
        self.assertIsNone(item['starts_at'])
        self.assertIsNone(item['ends_at'])
        self.assertIn('октябрь 2026', item['details']['internship']['duration_text'])
        self.assertEqual(item['requirements_text'], 'Основы Python')

    def test_permanent_positions_have_no_cards_or_report_entries(self):
        def fetch(_):
            self.fail('Must not fetch permanent vacancy details')
        collector = vk.Collector(fetch, WHEN)
        collector.internships({'vacancies': [{'id': 12, 'internship_type': 'junior'}]})
        self.assertEqual(collector.items, {})
        self.assertEqual(collector.mappings, [])

    def test_schema_break_is_an_error_not_an_empty_snapshot(self):
        collector = vk.Collector(lambda _: '<h1>Login required</h1>', WHEN)
        with self.assertRaises(vk.SeedError):
            collector.collect()
        with self.assertRaises(vk.SeedError):
            collector.internships({'vacancies': 'changed'})
        with self.assertRaises(vk.SeedError):
            collector.internships({'vacancies': [{'id': 14}]})
        with self.assertRaises(vk.SeedError):
            vk.Collector(lambda _: html({'programs': []}), WHEN).collect()
        with self.assertRaises(vk.SeedError):
            vk.date('октябрь 2026')

    def test_source_urls_do_not_follow_arbitrary_hosts_or_token_queries(self):
        for url in ['http://education.vk.company/program/1', 'https://evil.test/',
                    'https://education.vk.company.evil.test/', 'https://education.vk.company/program/1?token=secret']:
            with self.subTest(url=url), self.assertRaises(vk.SeedError):
                vk.safe_url(url, vk.FETCH_HOSTS)

    def test_activity_uses_source_dates_and_unknown_format(self):
        collector = vk.Collector(lambda _: '', WHEN)
        collector.activities({'activities': [{'id': 13, 'name': 'Олимпиада по ИИ', 'description': 'Решай задачи',
            'format': None, 'selection_status': 'closed', 'external_url': 'https://evil.test/apply',
            'selection_end_date': '2026-08-01T23:59:00+03:00'}]}, 'https://education.vk.company/centrum/olymp')
        item = collector.items['vk-activity-13']
        self.assertEqual(item['type'], 'event')
        self.assertEqual(item['format'], 'unknown')
        self.assertEqual(item['availability'], 'closed')
        self.assertEqual(item['action_url'], item['source_url'])
        self.assertIsNone(item['starts_at'])

    def test_projects_extracts_program_only_from_public_iframe(self):
        embedded = '<h1>Решай кейсы бизнес-юнитов VK</h1><p>Выбирай задачи: разработка и анализ данных</p>' \
                   '<h2>С чем помогут кейсы?</h2><p>Портфолио</p>' \
                   '<h2>Кто может участвовать в программе?</h2><p>Студенты вузов</p>' \
                   '<h2>Отзывы</h2><p>DO_NOT_STORE_REVIEW</p><script>DO_NOT_STORE_SCRIPT</script>'
        collector = vk.Collector(lambda _: embedded, WHEN)
        doc = vk.Document('<iframe src="https://tilda-embed.tech-mail.ru/example"></iframe>')
        collector.projects(doc, vk.SOURCES['projects'])
        item = collector.items['vk-education-projects']
        self.assertEqual(item['type'], 'course')
        self.assertEqual(item['requirements_text'], 'Студенты вузов')
        self.assertEqual(item['availability'], 'unknown')
        self.assertEqual(item['action_url'], vk.SOURCES['projects'])
        self.assertNotIn('DO_NOT_STORE', json.dumps(item))

    def test_collection_failure_preserves_existing_output_and_report(self):
        with tempfile.TemporaryDirectory() as directory:
            output, report = Path(directory) / 'catalog.json', Path(directory) / 'report.json'
            output.write_text('old catalog')
            report.write_text('old report')
            arguments = ['vk_catalog.py', '--output', str(output), '--report', str(report)]
            with patch('sys.argv', arguments), patch.object(vk, 'Fetcher', return_value=lambda _: '<h1>Changed page</h1>'), \
                    patch('sys.stderr'):
                self.assertEqual(vk.main(), 1)
            self.assertEqual(output.read_text(), 'old catalog')
            self.assertEqual(report.read_text(), 'old report')


if __name__ == '__main__':
    unittest.main()
