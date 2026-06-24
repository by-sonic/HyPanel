package core

import (
	"github.com/alireza0/s-ui/logger"
	"github.com/alireza0/s-ui/util/common"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/option"
	hysteria2 "github.com/sagernet/sing-box/protocol/hysteria2"
)

func (c *Core) AddInbound(config []byte) error {
	if !c.isRunning {
		return common.NewError("sing-box is not running")
	}
	var err error
	var inbound_config option.Inbound
	err = inbound_config.UnmarshalJSONContext(c.GetCtx(), config)
	if err != nil {
		return err
	}

	err = inbound_manager.Create(
		c.GetCtx(),
		router,
		factory.NewLogger("inbound/"+inbound_config.Type+"["+inbound_config.Tag+"]"),
		inbound_config.Tag,
		inbound_config.Type,
		inbound_config.Options)
	if err != nil {
		return err
	}

	return nil
}

func (c *Core) RemoveInbound(tag string) error {
	if !c.isRunning {
		return common.NewError("sing-box is not running")
	}
	logger.Info("remove inbound: ", tag)
	return inbound_manager.Remove(tag)
}

// hysteria2Inbound resolves a live hysteria2 inbound by tag for in-place user
// management (HyPanel Ф1). Returns an error for unknown or non-hysteria2 tags.
func (c *Core) hysteria2Inbound(tag string) (*hysteria2.Inbound, error) {
	if !c.isRunning {
		return nil, common.NewError("sing-box is not running")
	}
	inb, found := inbound_manager.Get(tag)
	if !found {
		return nil, common.NewError("inbound not found: ", tag)
	}
	h2inb, ok := inb.(*hysteria2.Inbound)
	if !ok {
		return nil, common.NewError("inbound is not hysteria2: ", tag, " (", inb.Type(), ")")
	}
	return h2inb, nil
}

// UpdateInboundUsers live-replaces the user set of a hysteria2 inbound WITHOUT
// removing+re-adding it, so connections of still-valid users are never dropped.
// names[i] authenticates with passwords[i]; traffic is accounted under names[i].
// After swapping the set it evicts any live session whose password is no longer
// present, so a disabled/banned/deleted user is kicked immediately (not merely
// blocked on the next handshake).
func (c *Core) UpdateInboundUsers(tag string, names, passwords []string) error {
	h2inb, err := c.hysteria2Inbound(tag)
	if err != nil {
		return err
	}
	logger.Info("update users for hysteria2 inbound: ", tag, " (", len(names), " entries)")
	h2inb.UpdateUsers(names, passwords)
	h2inb.RetainUsers(passwords)
	return nil
}

func (c *Core) AddOutbound(config []byte) error {
	if !c.isRunning {
		return common.NewError("sing-box is not running")
	}
	var err error
	var outbound_config option.Outbound

	err = outbound_config.UnmarshalJSONContext(c.GetCtx(), config)
	if err != nil {
		return err
	}

	outboundCtx := adapter.WithContext(c.GetCtx(), &adapter.InboundContext{
		Outbound: outbound_config.Tag,
	})

	err = outbound_manager.Create(
		outboundCtx,
		router,
		factory.NewLogger("outbound/"+outbound_config.Type+"["+outbound_config.Tag+"]"),
		outbound_config.Tag,
		outbound_config.Type,
		outbound_config.Options)
	if err != nil {
		return err
	}

	return nil
}

func (c *Core) RemoveOutbound(tag string) error {
	if !c.isRunning {
		return common.NewError("sing-box is not running")
	}
	logger.Info("remove outbound: ", tag)
	return outbound_manager.Remove(tag)
}

func (c *Core) AddEndpoint(config []byte) error {
	if !c.isRunning {
		return common.NewError("sing-box is not running")
	}
	var err error
	var endpoint_config option.Endpoint

	err = endpoint_config.UnmarshalJSONContext(c.GetCtx(), config)
	if err != nil {
		return err
	}

	err = endpoint_manager.Create(
		c.GetCtx(),
		router,
		factory.NewLogger("endpoint/"+endpoint_config.Type+"["+endpoint_config.Tag+"]"),
		endpoint_config.Tag,
		endpoint_config.Type,
		endpoint_config.Options)
	if err != nil {
		return err
	}

	return nil
}

func (c *Core) RemoveEndpoint(tag string) error {
	if !c.isRunning {
		return common.NewError("sing-box is not running")
	}
	logger.Info("remove endpoint: ", tag)
	return endpoint_manager.Remove(tag)
}

func (c *Core) AddService(config []byte) error {
	if !c.isRunning {
		return common.NewError("sing-box is not running")
	}
	var err error
	var srv_config option.Service

	err = srv_config.UnmarshalJSONContext(c.GetCtx(), config)
	if err != nil {
		return err
	}

	err = service_manager.Create(
		c.GetCtx(),
		factory.NewLogger("service/"+srv_config.Type+"["+srv_config.Tag+"]"),
		srv_config.Tag,
		srv_config.Type,
		srv_config.Options)
	if err != nil {
		return err
	}

	return nil
}

func (c *Core) RemoveService(tag string) error {
	if !c.isRunning {
		return common.NewError("sing-box is not running")
	}
	logger.Info("remove service: ", tag)
	return service_manager.Remove(tag)
}
