package datacoord

import (
	"testing"

	"github.com/pingcap/log"
	"github.com/samber/lo"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
	"go.uber.org/zap"

	"github.com/milvus-io/milvus/internal/proto/datapb"
)

func TestCompactionTriggerManagerSuite(t *testing.T) {
	suite.Run(t, new(CompactionTriggerManagerSuite))
}

type CompactionTriggerManagerSuite struct {
	suite.Suite

	mockAlloc       *NMockAllocator
	mockPlanContext *MockCompactionPlanContext
	testLabel       *CompactionGroupLabel
	meta            *meta

	m *CompactionTriggerManager
}

func (s *CompactionTriggerManagerSuite) SetupTest() {
	s.mockAlloc = NewNMockAllocator(s.T())
	s.mockPlanContext = NewMockCompactionPlanContext(s.T())

	s.testLabel = &CompactionGroupLabel{
		CollectionID: 1,
		PartitionID:  10,
		Channel:      "ch-1",
	}
	s.meta = &meta{segments: &SegmentsInfo{
		segments: genSegmentsForMeta(s.testLabel),
	}}

	s.m = NewCompactionTriggerManager(s.mockAlloc, s.mockPlanContext)
}

func (s *CompactionTriggerManagerSuite) TestNotifyByViewIDLE() {
	viewManager := NewCompactionViewManager(s.meta, s.m, s.m.allocator)
	collSegs := s.meta.GetCompactableSegmentGroupByCollection()

	segments, found := collSegs[1]
	s.Require().True(found)

	seg1, found := lo.Find(segments, func(info *SegmentInfo) bool {
		return info.ID == int64(100) && info.GetLevel() == datapb.SegmentLevel_L0
	})
	s.Require().True(found)

	// Prepare only 1 l0 segment that doesn't meet the Trigger minimum condition
	// but ViewIDLE Trigger will still forceTrigger the plan
	latestL0Segments := GetViewsByInfo(seg1)
	expectedSegID := seg1.ID

	s.Require().Equal(1, len(latestL0Segments))
	levelZeroView := viewManager.getChangedLevelZeroViews(1, latestL0Segments)
	s.Require().Equal(1, len(levelZeroView))
	cView, ok := levelZeroView[0].(*LevelZeroSegmentsView)
	s.True(ok)
	s.NotNil(cView)
	log.Info("view", zap.Any("cView", cView))

	s.mockAlloc.EXPECT().allocID(mock.Anything).Return(1, nil)
	s.mockPlanContext.EXPECT().execCompactionPlan(mock.Anything, mock.Anything).
		Run(func(signal *compactionSignal, plan *datapb.CompactionPlan) {
			s.EqualValues(19530, signal.id)
			s.True(signal.isGlobal)
			s.False(signal.isForce)
			s.EqualValues(30000, signal.pos.GetTimestamp())
			s.Equal(s.testLabel.CollectionID, signal.collectionID)
			s.Equal(s.testLabel.PartitionID, signal.partitionID)

			s.NotNil(plan)
			s.Equal(s.testLabel.Channel, plan.GetChannel())
			s.Equal(datapb.CompactionType_Level0DeleteCompaction, plan.GetType())

			expectedSegs := []int64{expectedSegID}
			gotSegs := lo.Map(plan.GetSegmentBinlogs(), func(b *datapb.CompactionSegmentBinlogs, _ int) int64 {
				return b.GetSegmentID()
			})

			s.ElementsMatch(expectedSegs, gotSegs)
			log.Info("generated plan", zap.Any("plan", plan))
		}).Return(nil).Once()

	s.m.Notify(19530, TriggerTypeLevelZeroViewIDLE, levelZeroView)
}

func (s *CompactionTriggerManagerSuite) TestNotifyByViewChange() {
	viewManager := NewCompactionViewManager(s.meta, s.m, s.m.allocator)
	collSegs := s.meta.GetCompactableSegmentGroupByCollection()

	segments, found := collSegs[1]
	s.Require().True(found)

	levelZeroSegments := lo.Filter(segments, func(info *SegmentInfo, _ int) bool {
		return info.GetLevel() == datapb.SegmentLevel_L0
	})

	latestL0Segments := GetViewsByInfo(levelZeroSegments...)
	s.Require().NotEmpty(latestL0Segments)
	levelZeroView := viewManager.getChangedLevelZeroViews(1, latestL0Segments)
	s.Require().Equal(1, len(levelZeroView))
	cView, ok := levelZeroView[0].(*LevelZeroSegmentsView)
	s.True(ok)
	s.NotNil(cView)
	log.Info("view", zap.Any("cView", cView))

	s.mockAlloc.EXPECT().allocID(mock.Anything).Return(1, nil)
	s.mockPlanContext.EXPECT().execCompactionPlan(mock.Anything, mock.Anything).
		Run(func(signal *compactionSignal, plan *datapb.CompactionPlan) {
			s.EqualValues(19530, signal.id)
			s.True(signal.isGlobal)
			s.False(signal.isForce)
			s.EqualValues(30000, signal.pos.GetTimestamp())
			s.Equal(s.testLabel.CollectionID, signal.collectionID)
			s.Equal(s.testLabel.PartitionID, signal.partitionID)

			s.NotNil(plan)
			s.Equal(s.testLabel.Channel, plan.GetChannel())
			s.Equal(datapb.CompactionType_Level0DeleteCompaction, plan.GetType())

			expectedSegs := []int64{100, 101, 102}
			gotSegs := lo.Map(plan.GetSegmentBinlogs(), func(b *datapb.CompactionSegmentBinlogs, _ int) int64 {
				return b.GetSegmentID()
			})

			s.ElementsMatch(expectedSegs, gotSegs)
			log.Info("generated plan", zap.Any("plan", plan))
		}).Return(nil).Once()

	s.m.Notify(19530, TriggerTypeLevelZeroViewChange, levelZeroView)
}
