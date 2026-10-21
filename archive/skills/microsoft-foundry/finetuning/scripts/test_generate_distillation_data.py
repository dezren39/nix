import os
import sys
import pytest
from unittest.mock import MagicMock, patch

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import generate_distillation_data


def test_grade_output_mocked():
    mock_client = MagicMock()
    mock_resp = MagicMock()
    mock_resp.choices = [
        MagicMock(message=MagicMock(content='{"accuracy": 9, "quality": 8, "task_fit": 10}'))
    ]
    mock_client.chat.completions.create.return_value = mock_resp

    scores = generate_distillation_data.grade_output(mock_client, "gpt-4o", "Sample output")
    assert scores == {"accuracy": 9, "quality": 8, "task_fit": 10}


def test_concurrent_grading_step():
    mock_client = MagicMock()

    def mock_grade(client, judge_model, output):
        return {"accuracy": 8, "quality": 8, "task_fit": 8}

    examples = [{"response": f"response {i}"} for i in range(10)]

    with patch.object(generate_distillation_data, "grade_output", side_effect=mock_grade):
        def grade_one(idx):
            ex = examples[idx]
            scores = generate_distillation_data.grade_output(mock_client, "judge", ex["response"])
            return idx, scores

        with generate_distillation_data.ThreadPoolExecutor(max_workers=4) as pool:
            futures = {pool.submit(grade_one, i): i for i in range(len(examples))}
            for future in generate_distillation_data.as_completed(futures):
                idx, scores = future.result()
                ex = examples[idx]
                if scores:
                    ex["scores"] = scores
                    ex["avg_score"] = sum(scores.values()) / len(scores)

    for ex in examples:
        assert ex["scores"] == {"accuracy": 8, "quality": 8, "task_fit": 8}
        assert ex["avg_score"] == 8.0
