package state

import (
	"errors"
	"sync"

	rafttypes "github.com/Dataman-Cloud/swan/src/manager/raft/types"
	"github.com/Dataman-Cloud/swan/src/mesosproto/mesos"

	"github.com/Sirupsen/logrus"
	"golang.org/x/net/context"
)

var instance *OfferAllocator
var once sync.Once

type OfferInfo struct {
	OfferID  string
	AgentID  string
	Hostname string
	AgentIP  string
}

type OfferAllocator struct {
	PendingOfferSlots  []*Slot
	pendingOfferRWLock sync.RWMutex

	// slotid -> offerinfo
	AllocatedOffer     map[string]*OfferInfo // we store every offer that are occupied by running slot
	allocatedOfferLock sync.RWMutex
}

func OfferAllocatorInstance() *OfferAllocator {
	once.Do(func() {
		instance = &OfferAllocator{
			PendingOfferSlots: make([]*Slot, 0),
			AllocatedOffer:    make(map[string]*OfferInfo),
		}
	})

	return instance
}

func (allocator *OfferAllocator) ShiftNextPendingOffer() *Slot {
	if len(allocator.PendingOfferSlots) == 0 {
		return nil
	}

	var slot *Slot
	allocator.pendingOfferRWLock.Lock()
	slot, allocator.PendingOfferSlots = allocator.PendingOfferSlots[0], allocator.PendingOfferSlots[1:]
	allocator.pendingOfferRWLock.Unlock()

	return slot
}

func (allocator *OfferAllocator) PutSlotBackToPendingQueue(slot *Slot) {
	allocator.pendingOfferRWLock.Lock()
	allocator.PendingOfferSlots = append(allocator.PendingOfferSlots, slot)
	allocator.pendingOfferRWLock.Unlock()
}

func (allocator *OfferAllocator) RemoveSlotFromPendingOfferQueue(slot *Slot) {
	allocator.pendingOfferRWLock.Lock()
	defer allocator.pendingOfferRWLock.Unlock()

	slotIndex := -1

	for index, pendingOfferSlot := range allocator.PendingOfferSlots {
		if pendingOfferSlot.ID == slot.ID {
			slotIndex = index
			break
		}
	}

	if slotIndex == -1 {
		return
	}

	allocator.PendingOfferSlots = append(allocator.PendingOfferSlots[:slotIndex], allocator.PendingOfferSlots[slotIndex+1:]...)
}

// NOTE Lock & raft write may cause performance problems
func (allocator *OfferAllocator) SetOfferSlotMap(offer *mesos.Offer, slot *Slot) {
	allocator.allocatedOfferLock.Lock()
	info := &OfferInfo{
		OfferID:  *offer.GetId().Value,
		AgentID:  *offer.GetAgentId().Value,
		Hostname: offer.GetHostname(),
	}
	allocator.create(slot.ID, info) // TODO error dealing
	allocator.AllocatedOffer[slot.ID] = info
	allocator.allocatedOfferLock.Unlock()
}

// NOTE Lock & raft write may cause performance problems
func (allocator *OfferAllocator) RemoveOfferSlotMapBySlot(slot *Slot) {
	allocator.allocatedOfferLock.Lock()
	allocator.remove(slot.ID)
	delete(allocator.AllocatedOffer, slot.ID)
	allocator.allocatedOfferLock.Unlock()
}

func (allocator *OfferAllocator) RemoveOfferSlotMapByOfferId(offerId string) {
	allocator.allocatedOfferLock.Lock()
	defer allocator.allocatedOfferLock.Unlock()

	key := ""
	for k, v := range allocator.AllocatedOffer {
		if v.OfferID == offerId {
			key = k
			break
		}
	}

	// shortcut execution if not found
	if len(key) == 0 {
		return
	}

	delete(allocator.AllocatedOffer, key)
}

func (allocator *OfferAllocator) RetrieveSlotIdByOfferId(offerId string) (string, error) {
	allocator.allocatedOfferLock.RLock()
	defer allocator.allocatedOfferLock.RUnlock()

	key := ""
	for k, v := range allocator.AllocatedOffer {
		if v.OfferID == offerId {
			key = k
			break
		}
	}

	if len(key) == 0 {
		return "", errors.New("slot not found")
	}

	return key, nil
}

func (allocator *OfferAllocator) SlotsByAgentID(agentID string) []string {
	allocator.allocatedOfferLock.RLock()
	defer allocator.allocatedOfferLock.RUnlock()

	slots := make([]string, 0)
	for slotID, info := range allocator.AllocatedOffer {
		if info.AgentID == agentID {
			slots = append(slots, slotID)
		}
	}

	return slots
}

func (allocator *OfferAllocator) SlotsByHostname(hostname string) []string {
	allocator.allocatedOfferLock.RLock()
	defer allocator.allocatedOfferLock.RUnlock()

	slots := make([]string, 0)
	for slotID, info := range allocator.AllocatedOffer {
		if info.Hostname == hostname {
			slots = append(slots, slotID)
		}
	}

	return slots
}

func (allocator *OfferAllocator) RemoveSlotFromAllocator(slot *Slot) {
	allocator.RemoveSlotFromPendingOfferQueue(slot)
	allocator.RemoveOfferSlotMapBySlot(slot)
}

func (allocator *OfferAllocator) create(slotID string, offerInfo *OfferInfo) {
	logrus.Debugf("create offer allocator item %s => %s", slotID, offerInfo.OfferID)
	persistentStore.CreateOfferAllocatorItem(context.TODO(), &rafttypes.OfferAllocatorItem{
		OfferID:  offerInfo.OfferID,
		SlotID:   slotID,
		AgentID:  offerInfo.AgentID,
		Hostname: offerInfo.Hostname}, nil)
}

func (allocator *OfferAllocator) remove(slotID string) {
	logrus.Debugf("remove offer allocator item  %s", slotID)
	persistentStore.DeleteOfferAllocatorItem(context.TODO(), slotID, nil)
}
