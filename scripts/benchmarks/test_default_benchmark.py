import json
import tempfile
import threading
import unittest
from pathlib import Path
from urllib.request import Request, urlopen

import run_default_benchmark as benchmark


class ReviewFixtureValidation(unittest.TestCase):
    def test_named_and_anonymous_queries_observe_the_same_reply(self):
        with tempfile.TemporaryDirectory() as directory:
            server = benchmark.core.Backend(Path(directory))
            server.RequestHandlerClass = benchmark.Handler
            server.changed = False
            thread = threading.Thread(target=server.serve_forever, daemon=True)
            thread.start()
            base = f'http://127.0.0.1:{server.server_port}'

            def reviews(query):
                request = Request(base + '/graphql', data=json.dumps({'query': query}).encode(),
                                  headers={'Content-Type': 'application/json'})
                with urlopen(request) as response:
                    return json.load(response)['data']['repository']['pullRequest']['reviewThreads']['nodes']

            try:
                guide = benchmark.prompt('reviews', 'gh')
                anonymous = guide.split('GraphQL query: ', 1)[1].split('. Use gh api', 1)[0]
                self.assertEqual(anonymous.count('{'), anonymous.count('}'))
                named = anonymous.replace('query {', 'query PullRequestReviews {', 1)
                original = reviews(anonymous)
                self.assertEqual(len(original), 20)
                self.assertEqual(original, reviews(named))
                with urlopen(base + '/fixture/advance') as response:
                    self.assertEqual(json.load(response), {'changed': True})
                updated = reviews(anonymous)
                self.assertEqual(updated, reviews(named))
                previous_ids = {c['id'] for t in original for c in t['comments']['nodes']}
                added = [{'id': c['id'], 'thread_id': t['id'], 'body': c['body']}
                         for t in updated for c in t['comments']['nodes'] if c['id'] not in previous_ids]
                self.assertEqual(added, benchmark.expected('reviews')['new_comments'])
            finally:
                server.shutdown()
                server.server_close()
                thread.join()


if __name__ == '__main__':
    unittest.main()
