from __future__ import annotations

import math


def mean_vectors(vectors: list[list[float]]) -> list[float]:
    if not vectors:
        raise ValueError("no vectors to average")

    dim = len(vectors[0])
    if dim == 0:
        raise ValueError("empty embedding vector")

    summed = [0.0] * dim
    for vector in vectors:
        if len(vector) != dim:
            raise ValueError("embedding dimensions differ")
        for idx, value in enumerate(vector):
            value = float(value)
            if not math.isfinite(value):
                raise ValueError("embedding contains non-finite value")
            summed[idx] += value

    count = float(len(vectors))
    return [value / count for value in summed]


def normalize(vector: list[float]) -> list[float]:
    norm = math.sqrt(sum(value * value for value in vector))
    if norm == 0 or not math.isfinite(norm):
        raise ValueError("cannot normalize embedding vector")
    return [float(value / norm) for value in vector]
