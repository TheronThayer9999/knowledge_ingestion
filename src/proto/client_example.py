# Mẫu client Python cho AI agent gọi KnowledgeService qua gRPC.
#
# 1. Sinh stubs từ proto (1 lần, mỗi khi knowledge.proto đổi):
#      pip install grpcio grpcio-tools
#      python -m grpc_tools.protoc -Isrc/proto \
#        --python_out=. --grpc_python_out=. \
#        src/proto/knowledge/v1/knowledge.proto
#
# 2. Lấy JWT trước qua HTTP: POST /api/v1/auth/login -> token.
# 3. Chạy file này: python proto/client_example.py
"""Example Python client for the Knowledge gRPC service (AI agent side)."""

import grpc

from knowledge.v1 import knowledge_pb2, knowledge_pb2_grpc

GRPC_ADDR = "localhost:9002"


def main() -> None:
    # TODO: lấy token thật từ POST /api/v1/auth/login.
    token = "PASTE_JWT_HERE"

    channel = grpc.insecure_channel(GRPC_ADDR)
    stub = knowledge_pb2_grpc.KnowledgeServiceStub(channel)

    # Metadata tương đương header HTTP: authorization Bearer + trace nối log.
    metadata = (
        ("authorization", f"Bearer {token}"),
        ("x-trace-id", "agent-run-001"),
    )

    # Search: hỏi tri thức trong phạm vi bài của chính mình.
    resp = stub.Search(
        knowledge_pb2.SearchRequest(query="Go Fx quản lý vòng đời thế nào", limit=5),
        metadata=metadata,
    )
    print(f"total={resp.total}")
    for hit in resp.hits:
        print(f"[{hit.score:.3f}] article={hit.article_id} chunk={hit.chunk_index} page={hit.page_num}")
        print(f"  {hit.text[:200]}")

    # Rebuild: đưa bài về hàng đợi chunk→embed từ đầu (Qdrant mất/chuyển cụm).
    rebuild = stub.RebuildVectors(
        knowledge_pb2.RebuildVectorsRequest(article_ids=[8, 9]),
        metadata=metadata,
    )
    print(f"reset_articles={rebuild.reset_articles} ids={list(rebuild.article_ids)}")


if __name__ == "__main__":
    main()
