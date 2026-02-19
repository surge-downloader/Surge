import unittest
from datetime import datetime

import analyze


class AnalyzeTests(unittest.TestCase):
    def test_parse_duration_mixed_units(self):
        self.assertAlmostEqual(analyze.parse_duration("1m30s"), 90.0)
        self.assertAlmostEqual(analyze.parse_duration("500ms"), 0.5)
        self.assertAlmostEqual(analyze.parse_duration("2.5s"), 2.5)

    def test_percentile(self):
        values = [1.0, 2.0, 3.0, 4.0]
        self.assertAlmostEqual(analyze.percentile(values, 0.5), 2.5)
        self.assertAlmostEqual(analyze.percentile(values, 0.95), 3.85)

    def test_filter_data_worker_and_time(self):
        t1 = analyze.Task(datetime(2026, 2, 15, 10, 0, 1), 0, 1024, 1.0)
        t2 = analyze.Task(datetime(2026, 2, 15, 10, 0, 2), 1024, 1024, 1.0)
        w1 = analyze.WorkerStats(worker_id=1, start_time=datetime(2026, 2, 15, 10, 0, 0), end_time=datetime(2026, 2, 15, 10, 0, 3), tasks=[t1])
        w2 = analyze.WorkerStats(worker_id=2, start_time=datetime(2026, 2, 15, 10, 0, 0), end_time=datetime(2026, 2, 15, 10, 0, 3), tasks=[t2])

        data = {
            "workers": {1: w1, 2: w2},
            "balancer_splits": [],
            "health_kills": [],
            "download_info": {},
        }

        filtered = analyze.filter_data(
            data,
            worker_filter=2,
            since=datetime(2026, 2, 15, 10, 0, 1),
            until=datetime(2026, 2, 15, 10, 0, 2),
        )

        self.assertEqual(list(filtered["workers"].keys()), [2])
        self.assertEqual(len(filtered["workers"][2].tasks), 1)

    def test_throughput_buckets(self):
        tasks = [
            analyze.Task(datetime(2026, 2, 15, 10, 0, 0), 0, 2 * analyze.MB, 1.0),
            analyze.Task(datetime(2026, 2, 15, 10, 0, 1), 2 * analyze.MB, 2 * analyze.MB, 1.0),
            analyze.Task(datetime(2026, 2, 15, 10, 0, 3), 4 * analyze.MB, 2 * analyze.MB, 1.0),
        ]
        worker = analyze.WorkerStats(worker_id=1, tasks=tasks)
        ctx = analyze.ReportContext(
            data={"workers": {1: worker}, "balancer_splits": [], "health_kills": [], "download_info": {}},
            workers=[worker],
            all_tasks=tasks,
            global_avg_speed=0.0,
            global_avg_task_duration=0.0,
            speed_by_worker={1: worker.avg_speed_mbps},
            slow_tasks=[],
        )

        buckets = analyze.build_throughput_buckets(ctx, 2)
        self.assertEqual(len(buckets), 2)
        self.assertEqual(buckets[0]["bytes"], 4 * analyze.MB)
        self.assertEqual(buckets[1]["bytes"], 2 * analyze.MB)

    def test_health_event_impact(self):
        tasks = [
            analyze.Task(datetime(2026, 2, 15, 10, 0, 5), 0, 2 * analyze.MB, 2.0),
            analyze.Task(datetime(2026, 2, 15, 10, 0, 15), 2 * analyze.MB, 2 * analyze.MB, 1.0),
        ]
        worker = analyze.WorkerStats(worker_id=1, tasks=tasks)
        ctx = analyze.ReportContext(
            data={
                "workers": {1: worker},
                "balancer_splits": [],
                "health_kills": [(datetime(2026, 2, 15, 10, 0, 10), 1, "slow")],
                "download_info": {},
            },
            workers=[worker],
            all_tasks=tasks,
            global_avg_speed=0.0,
            global_avg_task_duration=0.0,
            speed_by_worker={1: worker.avg_speed_mbps},
            slow_tasks=[],
        )

        impacts = analyze.compute_health_event_impact(ctx, 10)
        self.assertEqual(len(impacts), 1)
        self.assertGreater(impacts[0]["after_avg_speed_mbps"], impacts[0]["before_avg_speed_mbps"])


if __name__ == "__main__":
    unittest.main()
