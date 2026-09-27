from copy import deepcopy


class FakeStore:
    def __init__(self):
        self.records = {}
    async def ping(self):
        pass
    async def save(self, record):
        self.records[record['id']] = deepcopy(record)
    async def load(self):
        return deepcopy(list(self.records.values()))
    async def close(self):
        pass


async def wait_for_operation(job):
    import asyncio
    async with asyncio.timeout(3):
        while job.state in {"importing", "reauthorizing"} or job.manual_pending:
            await asyncio.sleep(0.005)
